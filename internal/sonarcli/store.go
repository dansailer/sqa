package sonarcli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Environment variables read by the sonar CLI that sqa sets for it.
const (
	// EnvKeychainFile makes sonar keep tokens in a JSON file instead of the OS
	// keychain (libsecret, macOS Keychain, Windows Credential Manager).
	EnvKeychainFile = "SONARQUBE_CLI_KEYCHAIN_FILE"
	// EnvSonarHome moves sonar's state (state.json, logs, telemetry) out of
	// ~/.sonar.
	EnvSonarHome = "SONAR_USER_HOME"
	// EnvDoNotTrack=1 turns off sonar's usage telemetry (events.sonardata.io)
	// and its Sentry error reports; both check it (isTelemetryEnabled).
	EnvDoNotTrack = "DO_NOT_TRACK"
)

// Store is where sonar keeps the token from `sonar auth login`.
type Store string

// Token stores.
const (
	StoreFile     Store = "file"
	StoreKeychain Store = "keychain"
)

// Origin says where a setting came from.
type Origin string

// Setting origins, in increasing precedence.
const (
	OriginDefault Origin = "default"
	OriginConfig  Origin = "config"
	OriginEnv     Origin = "environment"
)

// Permissions sqa enforces for the file token store.
const (
	CredentialsFileMode fs.FileMode = 0o600
	PrivateDirMode      fs.FileMode = 0o700
)

// Environment is the part of sonar's environment that sqa controls. sqa only
// ever handles the *path* of the credentials file and its permissions; it
// never opens the file for reading.
type Environment struct {
	Store Store
	// CredentialsFile is the token file (StoreFile only).
	CredentialsFile   string
	CredentialsOrigin Origin
	SonarHome         string
	SonarHomeOrigin   Origin
	// TelemetryOff is whether sonar's telemetry and error reporting are off.
	// With OriginDefault sqa sets DO_NOT_TRACK=1; with OriginEnv the user's
	// DO_NOT_TRACK is passed through; OriginConfig means sonar.telemetry: true.
	TelemetryOff    bool
	TelemetryOrigin Origin
}

// Settings are the inputs for ResolveEnvironment.
type Settings struct {
	Store                  string // auth.store: "", "file" or "keychain"
	CredentialsFile        string // auth.credentials_file
	SonarHome              string // sonar.home
	Telemetry              bool   // sonar.telemetry: let sonar send telemetry
	DefaultCredentialsFile string
	DefaultSonarHome       string
}

// ResolveEnvironment applies sqa's precedence: a non-empty variable in the
// user's environment wins (sonar reads it anyway), then config.yaml, then
// sqa's defaults. auth.store: keychain leaves the credentials file unset so
// sonar uses the OS keychain. DO_NOT_TRACK is respected whenever it is set
// at all (even to 0 or empty); otherwise telemetry is off unless
// sonar.telemetry is true. lookup is os.LookupEnv outside tests.
func ResolveEnvironment(lookup func(string) (string, bool), s Settings) (*Environment, error) {
	getenv := func(k string) string { v, _ := lookup(k); return v }
	e := &Environment{}
	switch v, ok := lookup(EnvDoNotTrack); {
	case ok:
		e.TelemetryOff, e.TelemetryOrigin = strings.TrimSpace(v) == "1", OriginEnv
	case s.Telemetry:
		e.TelemetryOff, e.TelemetryOrigin = false, OriginConfig
	default:
		e.TelemetryOff, e.TelemetryOrigin = true, OriginDefault
	}
	switch {
	case getenv(EnvKeychainFile) != "":
		e.Store, e.CredentialsFile, e.CredentialsOrigin = StoreFile, getenv(EnvKeychainFile), OriginEnv
	case s.Store == string(StoreKeychain):
		e.Store, e.CredentialsOrigin = StoreKeychain, OriginConfig
	case s.Store == "" || s.Store == string(StoreFile):
		e.Store = StoreFile
		if s.CredentialsFile != "" {
			e.CredentialsFile, e.CredentialsOrigin = s.CredentialsFile, OriginConfig
		} else {
			e.CredentialsFile, e.CredentialsOrigin = s.DefaultCredentialsFile, OriginDefault
		}
	default:
		return nil, fmt.Errorf("auth.store %q: want file or keychain", s.Store)
	}
	switch {
	case getenv(EnvSonarHome) != "":
		e.SonarHome, e.SonarHomeOrigin = getenv(EnvSonarHome), OriginEnv
	case s.SonarHome != "":
		e.SonarHome, e.SonarHomeOrigin = s.SonarHome, OriginConfig
	default:
		e.SonarHome, e.SonarHomeOrigin = s.DefaultSonarHome, OriginDefault
	}
	for _, p := range []string{e.CredentialsFile, e.SonarHome} {
		if p != "" && !filepath.IsAbs(p) {
			return nil, fmt.Errorf("%q must be an absolute path", p)
		}
	}
	return e, nil
}

// Env returns base with sqa's variables applied. Variables that came from the
// user's environment are left untouched; empty ones are replaced (sonar
// would treat an empty SONAR_USER_HOME as the current directory).
func (e *Environment) Env(base []string) []string {
	set := map[string]string{}
	if e.Store == StoreFile && e.CredentialsOrigin != OriginEnv {
		set[EnvKeychainFile] = e.CredentialsFile
	}
	if e.SonarHomeOrigin != OriginEnv {
		set[EnvSonarHome] = e.SonarHome
	}
	if e.TelemetryOrigin == OriginDefault {
		set[EnvDoNotTrack] = "1"
	}
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; ok {
			continue
		}
		out = append(out, kv)
	}
	for _, k := range []string{EnvKeychainFile, EnvSonarHome, EnvDoNotTrack} {
		if v, ok := set[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// Prepare runs before every sonar invocation. It creates sqa's directories
// with 0700 and, for the file store, creates an empty credentials file with
// 0600 when none exists. sonar rewrites the file in place (writeFileSync
// truncates; mode is kept), so a token is never written to a file other users
// can read, whatever the umask. An existing file is tightened to 0600.
func (e *Environment) Prepare() error {
	switch e.SonarHomeOrigin {
	case OriginDefault:
		if err := privateDir(e.SonarHome, true); err != nil {
			return err
		}
	case OriginConfig:
		if err := privateDir(e.SonarHome, false); err != nil {
			return err
		}
	}
	if e.Store != StoreFile {
		return nil
	}
	// A missing parent directory is created (0700) wherever the path came
	// from: sonar does not create it and fails the login with ENOENT. Only
	// sqa's default directory is tightened when it already exists.
	if err := privateDir(filepath.Dir(e.CredentialsFile), e.CredentialsOrigin == OriginDefault); err != nil {
		return err
	}
	f, err := os.OpenFile(e.CredentialsFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, CredentialsFileMode) //nolint:gosec // path is sqa's credentials file (config or env)
	switch {
	case err == nil:
		if err := f.Close(); err != nil {
			return err
		}
		_, err := TightenFile(e.CredentialsFile)
		return err
	case errors.Is(err, fs.ErrExist):
		_, err := e.Secure()
		return err
	default:
		return fmt.Errorf("credentials file: %w", err)
	}
}

// Secure makes the credentials file private if it exists (0600 on Unix, a
// user-only ACL on Windows) and reports the previous protection when it
// changed it. It runs after every sonar invocation as well, in case sonar
// (or anything else) replaced the file. It never reads the file.
func (e *Environment) Secure() (changed string, err error) {
	if e.Store != StoreFile {
		return "", nil
	}
	st, err := os.Stat(e.CredentialsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials file: %w", err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("credentials file %s is not a regular file", e.CredentialsFile)
	}
	changed, err = TightenFile(e.CredentialsFile)
	if err != nil {
		return "", fmt.Errorf("credentials file: %w", err)
	}
	return changed, nil
}

// privateDir creates dir (and parents) with 0700. With tighten, an existing
// dir is also made private (chmod 0700, or a user-only ACL on Windows); sqa
// only does that for its own directories.
func privateDir(dir string, tighten bool) error {
	if err := os.MkdirAll(dir, PrivateDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if !tighten {
		return nil
	}
	if _, err := TightenDir(dir); err != nil {
		return fmt.Errorf("protect %s: %w", dir, err)
	}
	return nil
}
