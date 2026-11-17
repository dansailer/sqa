package sonarcli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Bundle is an official sonar binary embedded in the sqa executable (release
// builds use the bundled_sonar build tag). It is extracted to disk on first
// use, because the OS can only exec a file.
type Bundle struct {
	Version  string // full sonar version, e.g. 1.12.0.17622
	Platform string // SonarSource platform name, e.g. linux-x86-64
	SHA256   string // expected digest of Data, compiled into sqa
	Data     []byte
}

// bundled is set by bundled_on.go when sqa is built with -tags bundled_sonar.
var bundled *Bundle

// Bundled returns the sonar binary embedded in this build, or nil.
func Bundled() *Bundle { return bundled }

// FileName is the name the bundle is extracted under (sonar-<version>, or
// sonar-<version>.exe on Windows), so upgrades of the bundled version never
// overwrite a binary that is in use.
func (b *Bundle) FileName() string {
	if b == nil {
		return ""
	}
	return "sonar-" + b.Version + fileSuffix(b.Platform)
}

// ExtractResult describes the extracted binary.
type ExtractResult struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Extracted bool   `json:"extracted"` // false when a verified copy was already present
}

// Extract makes sure dir/sonar-<version> exists and has the expected SHA-256.
//
// A verified copy is recognised by a stamp file (<binary>.verified) holding the
// digest, size and mtime seen at verification time. When size or mtime change
// the file is re-hashed, and when the hash differs it is replaced. Writes go to
// a temp file in dir, are verified after writing, and are renamed into place,
// so a concurrent sqa never sees a partial binary. With force, the binary is
// always rewritten.
func (b *Bundle) Extract(dir string, force bool) (*ExtractResult, error) {
	if b == nil || len(b.Data) == 0 {
		return nil, errors.New("no bundled sonar binary in this build")
	}
	if !reVersion.MatchString(b.Version) {
		return nil, fmt.Errorf("invalid bundled sonar version %q", b.Version)
	}
	want := strings.ToLower(b.SHA256)
	dest := filepath.Join(dir, b.FileName())
	res := &ExtractResult{Path: dest, SHA256: want}
	if !force {
		if ok, err := verifiedOnDisk(dest, want); err != nil {
			return nil, err
		} else if ok {
			return res, nil
		}
	}

	sum := sha256.Sum256(b.Data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("bundled sonar is corrupt: expected sha256 %s, got %s", want, got)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".sonar-extract-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b.Data); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("extract sonar: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("extract sonar: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if got, err := FileSHA256(tmp.Name()); err != nil {
		return nil, err
	} else if got != want {
		return nil, fmt.Errorf("extracted sonar failed verification: expected sha256 %s, got %s", want, got)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil { //nolint:gosec // executable binary
		return nil, err
	}
	if err := replaceFile(tmp.Name(), dest); err != nil {
		return nil, err
	}
	if err := writeStamp(dest, want); err != nil {
		return nil, err
	}
	res.Extracted = true
	pruneOld(dir, b.FileName())
	return res, nil
}

func stampPath(bin string) string { return bin + ".verified" }

func statKey(st os.FileInfo) string {
	return strconv.FormatInt(st.Size(), 10) + " " + strconv.FormatInt(st.ModTime().UnixNano(), 10)
}

// verifiedOnDisk reports whether dest exists with digest want. It hashes the
// file only when the stamp is missing or its size/mtime no longer match.
func verifiedOnDisk(dest, want string) (bool, error) {
	st, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() {
		return false, nil
	}
	// Windows has no Unix executable bit; skip this check there.
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		return false, nil
	}
	if stamp, err := os.ReadFile(stampPath(dest)); err == nil && strings.TrimSpace(string(stamp)) == want+" "+statKey(st) {
		return true, nil
	}
	got, err := FileSHA256(dest)
	if err != nil || got != want {
		return false, nil //nolint:nilerr // unreadable or modified: re-extract
	}
	return true, writeStamp(dest, want)
}

func writeStamp(dest, sum string) error {
	st, err := os.Stat(dest)
	if err != nil {
		return err
	}
	return os.WriteFile(stampPath(dest), []byte(sum+" "+statKey(st)+"\n"), 0o600)
}

// pruneOld removes binaries extracted by other sqa versions (best effort).
func pruneOld(dir, keep string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "sonar-[0-9]*"))
	for _, m := range matches {
		base := filepath.Base(m)
		if base == keep || base == keep+".verified" {
			continue
		}
		_ = os.Remove(m)
	}
}
