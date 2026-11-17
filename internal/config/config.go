// Package config loads the optional sqa configuration file and resolves the
// well-known directories sqa uses. config.yaml never contains secrets; the
// token file next to it (credentials.json) is written by sonar, not by sqa.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the content of ~/.config/sqa/config.yaml. All fields are optional.
type Config struct {
	DefaultProject string `yaml:"default_project"`
	Auth           Auth   `yaml:"auth"`
	Sonar          Sonar  `yaml:"sonar"`
	Skills         Skills `yaml:"skills"`
}

// Auth selects where sonar keeps the token from `sqa auth login`.
type Auth struct {
	// Store is "file" (default: a 0600 JSON file, like terraform or oc) or
	// "keychain" (the OS keychain; on Linux that needs libsecret and a
	// running Secret Service such as gnome-keyring).
	Store string `yaml:"store"`
	// CredentialsFile overrides the token file (default
	// $XDG_CONFIG_HOME/sqa/credentials.json).
	CredentialsFile string `yaml:"credentials_file"`
}

// Sonar configures the official SonarSource CLI that sqa wraps.
type Sonar struct {
	// Version overrides the pinned sonar CLI version. When it differs from the
	// bundled version, sqa uses the `sqa setup` download instead of the bundle.
	Version string `yaml:"version"`
	// Path points at a sonar binary to use instead of the managed one.
	Path string `yaml:"path"`
	// Home is sonar's SONAR_USER_HOME (default $XDG_DATA_HOME/sqa/sonar-home),
	// kept separate from ~/.sonar so a standalone sonar's state never leaks in.
	Home string `yaml:"home"`
	// Telemetry lets sonar send usage telemetry and Sentry error reports to
	// SonarSource. Off by default: sqa runs sonar with DO_NOT_TRACK=1 unless
	// DO_NOT_TRACK is already set in the environment.
	Telemetry bool `yaml:"telemetry"`
}

// Skills configures `sqa skill install`.
type Skills struct {
	DefaultDir string `yaml:"default_dir"`
}

// Home returns the user's home directory.
func Home() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return h, nil
}

func xdg(env, fallback string) (string, error) {
	if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
		return v, nil
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, fallback), nil
}

// ConfigDir is $XDG_CONFIG_HOME/sqa (default ~/.config/sqa).
func ConfigDir() (string, error) {
	d, err := xdg("XDG_CONFIG_HOME", ".config")
	return filepath.Join(d, "sqa"), err
}

// DataDir is $XDG_DATA_HOME/sqa (default ~/.local/share/sqa).
func DataDir() (string, error) {
	d, err := xdg("XDG_DATA_HOME", filepath.Join(".local", "share"))
	return filepath.Join(d, "sqa"), err
}

// StateDir is $XDG_STATE_HOME/sqa (default ~/.local/state/sqa).
func StateDir() (string, error) {
	d, err := xdg("XDG_STATE_HOME", filepath.Join(".local", "state"))
	return filepath.Join(d, "sqa"), err
}

// ManagedSonarPath is where `sqa setup` installs the sonar binary.
func ManagedSonarPath() (string, error) {
	d, err := DataDir()
	return filepath.Join(d, "bin", "sonar"), err
}

// SonarBinDir holds sqa-managed sonar binaries: the `sqa setup` download
// (sonar) and the extracted bundled binary (sonar-<version>).
func SonarBinDir() (string, error) {
	d, err := DataDir()
	return filepath.Join(d, "bin"), err
}

// CredentialsFile is the default token file for the file store.
func CredentialsFile() (string, error) {
	d, err := ConfigDir()
	return filepath.Join(d, "credentials.json"), err
}

// SonarHome is the default SONAR_USER_HOME sqa gives sonar.
func SonarHome() (string, error) {
	d, err := DataDir()
	return filepath.Join(d, "sonar-home"), err
}

// Path returns the config file path, honouring SQA_CONFIG.
func Path() (string, error) {
	if p := os.Getenv("SQA_CONFIG"); p != "" {
		return p, nil
	}
	d, err := ConfigDir()
	return filepath.Join(d, "config.yaml"), err
}

// Load reads the config file. A missing file yields an empty config.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFile(p)
}

// LoadFile reads one config file. A missing file yields an empty config.
func LoadFile(p string) (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(p) //nolint:gosec // path is the user's own config file
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	for _, f := range []*string{&c.Sonar.Path, &c.Sonar.Home, &c.Skills.DefaultDir, &c.Auth.CredentialsFile} {
		if *f, err = ExpandHome(*f); err != nil {
			return nil, err
		}
	}
	switch c.Auth.Store {
	case "", "file", "keychain":
	default:
		return nil, fmt.Errorf("config %s: auth.store %q: want file or keychain", p, c.Auth.Store)
	}
	return c, nil
}

// ExpandHome expands a leading "~/" to the user's home directory.
func ExpandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, err := Home()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}
