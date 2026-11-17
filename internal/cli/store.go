package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansailer/sqa/internal/sonarcli"
)

// sonarKeychainSource is the label sonar prints for a stored login. sonar
// uses it for its file store as well, so sqa relabels it.
const sonarKeychainSource = "OS Keychain"

// storeDescription names where sonar reads the stored login from.
func storeDescription(env *sonarcli.Environment) string {
	if env.Store == sonarcli.StoreKeychain {
		return "OS keychain (auth.store: keychain)"
	}
	d := "file " + env.CredentialsFile
	if env.CredentialsOrigin == sonarcli.OriginEnv {
		d += " (from " + sonarcli.EnvKeychainFile + ")"
	}
	return d
}

// authSource relabels sonar's "OS Keychain" source when the file store is in
// use; env-var sources are passed through.
func authSource(source string, env *sonarcli.Environment) string {
	if source == sonarKeychainSource && env != nil && env.Store == sonarcli.StoreFile {
		return "credentials file"
	}
	return source
}

// checkTokenStore inspects (and tightens) the token store for `sqa doctor`.
// It looks at paths and permissions only, never at the file's content.
func checkTokenStore(env *sonarcli.Environment) (status, detail string) {
	if env.Store == sonarcli.StoreKeychain {
		return "ok", storeDescription(env)
	}
	var fixed []string
	if env.CredentialsOrigin == sonarcli.OriginDefault {
		dir := filepath.Dir(env.CredentialsFile)
		if st, err := os.Stat(dir); err == nil && st.Mode().Perm() != sonarcli.PrivateDirMode {
			if err := os.Chmod(dir, sonarcli.PrivateDirMode); err != nil {
				return "fail", fmt.Sprintf("%s: %v", dir, err)
			}
			fixed = append(fixed, fmt.Sprintf("directory was %04o, set to 0700", st.Mode().Perm()))
		}
	}
	was, err := env.Secure()
	if err != nil {
		return "fail", err.Error()
	}
	if was != 0 {
		fixed = append(fixed, fmt.Sprintf("file was %04o, set to 0600", was))
	}
	d := storeDescription(env)
	switch _, err := os.Stat(env.CredentialsFile); {
	case errors.Is(err, fs.ErrNotExist):
		d += " (not created yet; `sqa auth login` writes it)"
	case err != nil:
		return "fail", err.Error()
	default:
		d += " (0600)"
	}
	if len(fixed) > 0 {
		return "warn", d + "; fixed permissions: " + strings.Join(fixed, ", ")
	}
	return "ok", d
}

// keychainHint maps sonar's OS keychain errors to a fix. Only relevant with
// auth.store: keychain; the file store never touches the keychain.
func keychainHint(env *sonarcli.Environment, msg string) string {
	if env == nil || env.Store != sonarcli.StoreKeychain {
		return ""
	}
	const back = " Or drop auth.store: keychain from config.yaml to use sqa's file store."
	switch {
	case strings.Contains(msg, "libsecret could not be loaded") || strings.Contains(msg, "MissingKeychainError") || strings.Contains(msg, "libsecret not available"):
		return "libsecret is not installed: install it and a Secret Service, e.g. `sudo apt install libsecret-1-0 gnome-keyring dbus-user-session`, then log out and in again." + back
	case strings.Contains(msg, "org.freedesktop.secrets"):
		return "no Secret Service is running: log in to a desktop session with gnome-keyring, or over SSH/WSL run `dbus-run-session -- bash` and then `gnome-keyring-daemon --unlock --components=secrets`." + back
	}
	return ""
}
