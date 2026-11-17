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
