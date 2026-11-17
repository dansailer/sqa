package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	c, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultProject != "" || c.Sonar.Path != "" {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestLoadExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "default_project: demo\nsonar:\n  version: \"1.2.3\"\n  path: ~/bin/sonar\nskills:\n  default_dir: ~/skills\n"
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultProject != "demo" || c.Sonar.Version != "1.2.3" {
		t.Fatalf("unexpected %+v", c)
	}
	if c.Sonar.Path != filepath.Join(home, "bin", "sonar") {
		t.Fatalf("path not expanded: %s", c.Sonar.Path)
	}
	if c.Skills.DefaultDir != filepath.Join(home, "skills") {
		t.Fatalf("skills dir not expanded: %s", c.Skills.DefaultDir)
	}
}

func TestXDGOverrides(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_STATE_HOME", d)
	s, err := StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if s != filepath.Join(d, "sqa") {
		t.Fatalf("got %s", s)
	}
}

func TestLoadAuthAndSonarHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "auth:\n  store: keychain\n  credentials_file: ~/creds.json\nsonar:\n  home: ~/sh\n"
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.Store != "keychain" || c.Auth.CredentialsFile != filepath.Join(home, "creds.json") || c.Sonar.Home != filepath.Join(home, "sh") {
		t.Fatalf("unexpected %+v", c)
	}
	if err := os.WriteFile(p, []byte("auth:\n  store: vault\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(p); err == nil {
		t.Fatal("unknown auth.store accepted")
	}
}

func TestDefaultTokenPaths(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(d, "c"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(d, "d"))
	c, _ := CredentialsFile()
	h, _ := SonarHome()
	if c != filepath.Join(d, "c", "sqa", "credentials.json") || h != filepath.Join(d, "d", "sqa", "sonar-home") {
		t.Fatalf("got %s %s", c, h)
	}
}

func TestDirOfWindows(t *testing.T) {
	getenv := func(k string) string {
		return map[string]string{
			"APPDATA":      `C:\Users\x\AppData\Roaming`,
			"LOCALAPPDATA": `C:\Users\x\AppData\Local`,
		}[k]
	}
	c, err := dirOf("config", "windows", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if c != filepath.Join(`C:\Users\x\AppData\Roaming`, "sqa") {
		t.Errorf("config %s", c)
	}
	d, err := dirOf("data", "windows", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if d != filepath.Join(`C:\Users\x\AppData\Local`, "sqa") {
		t.Errorf("data %s", d)
	}
	s, err := dirOf("state", "windows", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if s != d {
		t.Errorf("state %s, want same as data %s", s, d)
	}
}

func TestDirOfWindowsXDGWins(t *testing.T) {
	getenv := func(k string) string {
		return map[string]string{
			"APPDATA":         `C:\Users\x\AppData\Roaming`,
			"LOCALAPPDATA":    `C:\Users\x\AppData\Local`,
			"XDG_CONFIG_HOME": `D:\xdgconfig`,
			"XDG_DATA_HOME":   `D:\xdgdata`,
			"XDG_STATE_HOME":  `D:\xdgstate`,
		}[k]
	}
	c, _ := dirOf("config", "windows", getenv)
	d, _ := dirOf("data", "windows", getenv)
	s, _ := dirOf("state", "windows", getenv)
	if c != filepath.Join(`D:\xdgconfig`, "sqa") || d != filepath.Join(`D:\xdgdata`, "sqa") || s != filepath.Join(`D:\xdgstate`, "sqa") {
		t.Fatalf("config=%s data=%s state=%s", c, d, s)
	}
}

func TestExeSuffix(t *testing.T) {
	if exeSuffix("windows") != ".exe" {
		t.Fatal("windows")
	}
	if exeSuffix("linux") != "" || exeSuffix("darwin") != "" {
		t.Fatal("unix")
	}
}

func TestExpandHomeBackslash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ExpandHome(`~\sonar.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "sonar.exe") {
		t.Fatalf("got %s", got)
	}
}
