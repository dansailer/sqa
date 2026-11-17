package sonarcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DistributionBaseURL is SonarSource's official binary distribution for the
// SonarQube CLI (the same source used by SonarSource's install.sh).
const DistributionBaseURL = "https://binaries.sonarsource.com/Distribution/sonarqube-cli"

// PinnedVersion is the sonar CLI release sqa is tested against.
const PinnedVersion = "1.12.0.17622"

// pinnedChecksums are the SHA-256 digests SonarSource publishes next to each
// binary (<binary>.sha256) for PinnedVersion. Embedding them means a
// compromised download host cannot serve a matching binary + checksum pair.
var pinnedChecksums = map[string]string{
	"linux-x86-64": "c692e64dad89ea9e2388ec338dad47fa8703954e1c0ca8af111bc43e34afaa99",
	"linux-arm64":  "258cb106e50721f6e58ba0e0bccd1c88cdc1bfe6b394871dc6640dc43ca12fea",
	"macos-arm64":  "b64ce7175fb6cd63b277e6981052285fc4bb0437d98144ca754c054d66972838",
}

// Platform maps GOOS/GOARCH to SonarSource's artifact platform names.
// SonarSource publishes no macOS x86-64 build.
func Platform(goos, goarch string) (osDir, platform string, err error) {
	switch {
	case goos == "linux" && goarch == "amd64":
		return "linux", "linux-x86-64", nil
	case goos == "linux" && goarch == "arm64":
		return "linux", "linux-arm64", nil
	case goos == "darwin" && goarch == "arm64":
		return "macos", "macos-arm64", nil
	case goos == "darwin" && goarch == "amd64":
		return "", "", errors.New("SonarSource publishes no macOS x86-64 build of the sonar CLI; install it another way and set sonar.path in ~/.config/sqa/config.yaml")
	}
	return "", "", fmt.Errorf("unsupported platform %s/%s", goos, goarch)
}

var reVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}$`)

// Installer downloads and verifies the official sonar binary.
type Installer struct {
	BaseURL string
	Client  *http.Client
	GOOS    string
	GOARCH  string
}

// ArtifactURL returns the binary URL for version/platform.
func (in *Installer) ArtifactURL(version, osDir, platform string) string {
	return fmt.Sprintf("%s/%s/%s/sonarqube-cli-%s-%s.bin", strings.TrimRight(in.BaseURL, "/"), version, osDir, version, platform)
}

// InstallResult describes a finished install.
type InstallResult struct {
	Version        string `json:"version"`
	Platform       string `json:"platform"`
	URL            string `json:"url"`
	SHA256         string `json:"sha256"`
	ChecksumSource string `json:"checksum_source"`
	Path           string `json:"path"`
}

// Install downloads version for the configured platform, verifies its
// SHA-256 and atomically installs it at dest (mode 0755).
func (in *Installer) Install(ctx context.Context, version, dest string) (*InstallResult, error) {
	if !reVersion.MatchString(version) {
		return nil, fmt.Errorf("invalid sonar version %q", version)
	}
	osDir, platform, err := Platform(in.GOOS, in.GOARCH)
	if err != nil {
		return nil, err
	}
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	res := &InstallResult{Version: version, Platform: platform, URL: in.ArtifactURL(version, osDir, platform), Path: dest}

	expected, source := "", ""
	if version == PinnedVersion {
		expected, source = pinnedChecksums[platform], "embedded in sqa"
	}
	if expected == "" {
		sum, err := fetch(ctx, client, res.URL+".sha256", 1024)
		if err != nil {
			return nil, fmt.Errorf("fetch checksum: %w", err)
		}
		fields := strings.Fields(string(sum))
		if len(fields) == 0 {
			return nil, errors.New("empty checksum file")
		}
		expected, source = strings.ToLower(fields[0]), res.URL+".sha256"
	}
	res.ChecksumSource = source

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".sonar-download-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		_ = tmp.Close()
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("download %s: %w", res.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", res.URL, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 1<<30)); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("download %s: %w", res.URL, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	res.SHA256 = hex.EncodeToString(h.Sum(nil))
	if res.SHA256 != expected {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", res.URL, expected, res.SHA256)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil { //nolint:gosec // executable binary
		return nil, err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return nil, err
	}
	return res, nil
}

func fetch(ctx context.Context, c *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// FileSHA256 hashes a file.
func FileSHA256(p string) (string, error) {
	f, err := os.Open(p) //nolint:gosec // caller-supplied binary path
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ShortVersion turns "1.12.0.17622" into "1.12.0" (what `sonar --version`
// prints).
func ShortVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, ".")
}

// PinnedChecksum returns the embedded SHA-256 for PinnedVersion on platform.
func PinnedChecksum(platform string) string { return pinnedChecksums[platform] }
