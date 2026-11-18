//go:build !windows

package sonarcli

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestPrepareCreatesPrivateFileAndDirs(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	e, err := ResolveEnvironment(getenv(nil), defaults(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, e.CredentialsFile); m != 0o600 {
		t.Errorf("credentials file %04o", m)
	}
	if m := mode(t, filepath.Dir(e.CredentialsFile)); m != 0o700 {
		t.Errorf("config dir %04o", m)
	}
	if m := mode(t, e.SonarHome); m != 0o700 {
		t.Errorf("sonar home %04o", m)
	}
	if st, _ := os.Stat(e.CredentialsFile); st.Size() != 0 {
		t.Errorf("pre-created file should be empty, size %d", st.Size())
	}
}

func TestPrepareTightensExisting(t *testing.T) {
	e, _ := ResolveEnvironment(getenv(nil), defaults(t.TempDir()))
	dir := filepath.Dir(e.CredentialsFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.CredentialsFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.CredentialsFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, e.CredentialsFile); m != 0o600 {
		t.Errorf("file %04o", m)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("dir %04o", m)
	}
}

func TestPrepareLeavesConfiguredParentAlone(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	s := defaults(t.TempDir())
	s.CredentialsFile = filepath.Join(parent, "creds.json")
	e, _ := ResolveEnvironment(getenv(nil), s)
	if err := e.Prepare(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, parent); m != 0o755 {
		t.Errorf("configured parent dir changed to %04o", m)
	}
	if m := mode(t, e.CredentialsFile); m != 0o600 {
		t.Errorf("file %04o", m)
	}
}

func TestPrepareEnvFileMissingParent(t *testing.T) {
	// sonar does not create the parent and fails `auth login` with ENOENT.
	p := filepath.Join(t.TempDir(), "missing", "creds.json")
	e, _ := ResolveEnvironment(getenv(map[string]string{EnvKeychainFile: p}), defaults(t.TempDir()))
	if err := e.Prepare(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Dir(p)); m != 0o700 {
		t.Errorf("parent dir %04o, want 0700", m)
	}
	if m := mode(t, p); m != 0o600 {
		t.Errorf("file %04o, want 0600", m)
	}
}

func TestPrepareEnvFileLeavesExistingParentAlone(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(parent, "creds.json")
	e, _ := ResolveEnvironment(getenv(map[string]string{EnvKeychainFile: p}), defaults(t.TempDir()))
	if err := e.Prepare(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, parent); m != 0o755 {
		t.Errorf("user's parent dir changed to %04o", m)
	}
}

func TestSecure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	e := &Environment{Store: StoreFile, CredentialsFile: p}
	if was, err := e.Secure(); err != nil || was != "" {
		t.Fatalf("missing file: %v %v", was, err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if was, err := e.Secure(); err != nil || was != "0644" {
		t.Fatalf("was %q err %v", was, err)
	}
	if was, _ := e.Secure(); was != "" {
		t.Fatalf("second Secure changed %q", was)
	}
	d := &Environment{Store: StoreFile, CredentialsFile: t.TempDir()}
	if _, err := d.Secure(); err == nil {
		t.Error("directory accepted as credentials file")
	}
}

func TestRunnerSecuresAfterRun(t *testing.T) {
	dir := t.TempDir()
	e, _ := ResolveEnvironment(getenv(nil), defaults(dir))
	// A stand-in for sonar that replaces the file with a world-readable one.
	script := filepath.Join(dir, "sonar")
	body := "#!/bin/sh\nrm -f \"$SONARQUBE_CLI_KEYCHAIN_FILE\"\n(umask 022; echo '{}' >\"$SONARQUBE_CLI_KEYCHAIN_FILE\")\necho \"$SONAR_USER_HOME\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	out, _, err := (&Runner{Bin: script, Env: e}).Run(context.Background(), "auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != e.SonarHome+"\n" {
		t.Errorf("SONAR_USER_HOME = %q", out)
	}
	if m := mode(t, e.CredentialsFile); m != 0o600 {
		t.Errorf("after run %04o", m)
	}
}
