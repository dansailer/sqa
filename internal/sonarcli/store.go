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
}

// Settings are the inputs for ResolveEnvironment.
type Settings struct {
	Store                  string // auth.store: "", "file" or "keychain"
	CredentialsFile        string // auth.credentials_file
	SonarHome              string // sonar.home
	DefaultCredentialsFile string
	DefaultSonarHome       string
}

// ResolveEnvironment applies sqa's precedence: a non-empty variable in the
// user's environment wins (sonar reads it anyway), then config.yaml, then
// sqa's defaults. auth.store: keychain leaves the credentials file unset so
// sonar uses the OS keychain.
func ResolveEnvironment(getenv func(string) string, s Settings) (*Environment, error) {
	e := &Environment{}
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
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; ok {
			continue
		}
		out = append(out, kv)
	}
	for _, k := range []string{EnvKeychainFile, EnvSonarHome} {
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
	if e.CredentialsOrigin != OriginEnv {
		if err := privateDir(filepath.Dir(e.CredentialsFile), e.CredentialsOrigin == OriginDefault); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(e.CredentialsFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, CredentialsFileMode) //nolint:gosec // path is sqa's credentials file (config or env)
	switch {
	case err == nil:
		return f.Close()
	case errors.Is(err, fs.ErrExist):
		_, err := e.Secure()
		return err
	case errors.Is(err, fs.ErrNotExist) && e.CredentialsOrigin == OriginEnv:
		return nil // parent dir is the user's business; sonar reports the error
	default:
		return fmt.Errorf("credentials file: %w", err)
	}
}

// Secure sets the credentials file to 0600 if it exists with any other
// mode, and reports the mode it had when it changed it. It runs after every
// sonar invocation as well, in case sonar (or anything else) replaced the
// file. It never reads the file.
func (e *Environment) Secure() (was fs.FileMode, err error) {
	if e.Store != StoreFile {
		return 0, nil
	}
	st, err := os.Stat(e.CredentialsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("credentials file: %w", err)
	}
	if !st.Mode().IsRegular() {
		return 0, fmt.Errorf("credentials file %s is not a regular file", e.CredentialsFile)
	}
	if st.Mode().Perm() == CredentialsFileMode {
		return 0, nil
	}
	if err := os.Chmod(e.CredentialsFile, CredentialsFileMode); err != nil {
		return 0, fmt.Errorf("credentials file: %w", err)
	}
	return st.Mode().Perm(), nil
}

// privateDir creates dir (and parents) with 0700. With tighten, an existing
// dir is chmodded to 0700 too; sqa only does that for its own directories.
func privateDir(dir string, tighten bool) error {
	if err := os.MkdirAll(dir, PrivateDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if !tighten {
		return nil
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st.Mode().Perm() != PrivateDirMode {
		if err := os.Chmod(dir, PrivateDirMode); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
	}
	return nil
}
