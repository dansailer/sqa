package sonarcli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func getenv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func defaults(dir string) Settings {
	return Settings{
		DefaultCredentialsFile: filepath.Join(dir, "cfg", "sqa", "credentials.json"),
		DefaultSonarHome:       filepath.Join(dir, "data", "sqa", "sonar-home"),
	}
}

func TestResolveEnvironmentPrecedence(t *testing.T) {
	s := defaults("/h")
	e, err := ResolveEnvironment(getenv(nil), s)
	if err != nil {
		t.Fatal(err)
	}
	if e.Store != StoreFile || e.CredentialsFile != s.DefaultCredentialsFile || e.CredentialsOrigin != OriginDefault ||
		e.SonarHome != s.DefaultSonarHome || e.SonarHomeOrigin != OriginDefault {
		t.Fatalf("defaults: %+v", e)
	}

	s.CredentialsFile, s.SonarHome = "/c/creds.json", "/c/home"
	e, _ = ResolveEnvironment(getenv(nil), s)
	if e.CredentialsFile != "/c/creds.json" || e.CredentialsOrigin != OriginConfig || e.SonarHome != "/c/home" || e.SonarHomeOrigin != OriginConfig {
		t.Fatalf("config: %+v", e)
	}

	env := map[string]string{EnvKeychainFile: "/e/creds.json", EnvSonarHome: "/e/home"}
	s.Store = "keychain" // the user's env var wins over config, as it would in sonar
	e, _ = ResolveEnvironment(getenv(env), s)
	if e.Store != StoreFile || e.CredentialsFile != "/e/creds.json" || e.CredentialsOrigin != OriginEnv || e.SonarHome != "/e/home" || e.SonarHomeOrigin != OriginEnv {
		t.Fatalf("env: %+v", e)
	}

	e, _ = ResolveEnvironment(getenv(nil), s)
	if e.Store != StoreKeychain || e.CredentialsFile != "" {
		t.Fatalf("keychain: %+v", e)
	}

	if _, err := ResolveEnvironment(getenv(nil), Settings{Store: "vault"}); err == nil {
		t.Error("unknown store accepted")
	}
	if _, err := ResolveEnvironment(getenv(map[string]string{EnvSonarHome: "rel"}), defaults("/h")); err == nil {
		t.Error("relative sonar home accepted")
	}
}

func TestEnvironmentEnv(t *testing.T) {
	base := []string{"PATH=/bin", EnvSonarHome + "=", "SONARQUBE_CLI_TOKEN=x"}
	e := &Environment{Store: StoreFile, CredentialsFile: "/c/f.json", CredentialsOrigin: OriginDefault, SonarHome: "/d/h", SonarHomeOrigin: OriginDefault}
	got := e.Env(base)
	want := []string{"PATH=/bin", "SONARQUBE_CLI_TOKEN=x", EnvKeychainFile + "=/c/f.json", EnvSonarHome + "=/d/h"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}

	// Values from the user's environment are passed through untouched.
	base = []string{EnvKeychainFile + "=/u/f.json", EnvSonarHome + "=/u/h"}
	e = &Environment{Store: StoreFile, CredentialsFile: "/u/f.json", CredentialsOrigin: OriginEnv, SonarHome: "/u/h", SonarHomeOrigin: OriginEnv}
	if got := e.Env(base); !slices.Equal(got, base) {
		t.Fatalf("env origin: %v", got)
	}

	// The keychain store must not point sonar at a file.
	e = &Environment{Store: StoreKeychain, SonarHome: "/d/h", SonarHomeOrigin: OriginDefault}
	for _, kv := range e.Env([]string{"A=1"}) {
		if strings.HasPrefix(kv, EnvKeychainFile+"=") {
			t.Fatalf("keychain store set %s", kv)
		}
	}
}

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
	p := filepath.Join(t.TempDir(), "missing", "creds.json")
	e, _ := ResolveEnvironment(getenv(map[string]string{EnvKeychainFile: p}), defaults(t.TempDir()))
	if err := e.Prepare(); err != nil {
		t.Fatalf("missing parent of a user-set file should be left to sonar: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Error("sqa created the parent of a user-set credentials file")
	}
}

func TestSecure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	e := &Environment{Store: StoreFile, CredentialsFile: p}
	if was, err := e.Secure(); err != nil || was != 0 {
		t.Fatalf("missing file: %v %v", was, err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if was, err := e.Secure(); err != nil || was != 0o644 {
		t.Fatalf("was %04o err %v", was, err)
	}
	if was, _ := e.Secure(); was != 0 {
		t.Fatalf("second Secure changed %04o", was)
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

func TestTelemetryOffByDefault(t *testing.T) {
	has := func(env []string, kv string) bool { return slices.Contains(env, kv) }
	e, _ := ResolveEnvironment(getenv(nil), defaults("/h"))
	if !e.TelemetryOff || e.TelemetryOrigin != OriginDefault || !has(e.Env([]string{"A=1"}), "DO_NOT_TRACK=1") {
		t.Fatalf("default: %+v", e)
	}

	s := defaults("/h")
	s.Telemetry = true
	e, _ = ResolveEnvironment(getenv(nil), s)
	if e.TelemetryOff || e.TelemetryOrigin != OriginConfig {
		t.Fatalf("opt-in: %+v", e)
	}
	for _, kv := range e.Env([]string{"A=1"}) {
		if strings.HasPrefix(kv, EnvDoNotTrack+"=") {
			t.Fatalf("opt-in still sets %s", kv)
		}
	}

	// Any explicit DO_NOT_TRACK wins over sqa's default and the config, even 0 or empty.
	for v, off := range map[string]bool{"1": true, " 1 ": true, "0": false, "": false, "true": false} {
		for _, opt := range []bool{false, true} {
			s.Telemetry = opt
			e, _ = ResolveEnvironment(getenv(map[string]string{EnvDoNotTrack: v}), s)
			base := []string{EnvDoNotTrack + "=" + v}
			if e.TelemetryOff != off || e.TelemetryOrigin != OriginEnv || !slices.Equal(e.Env(base)[:1], base) || len(e.Env(base)) != 3 {
				t.Errorf("DO_NOT_TRACK=%q telemetry=%v: %+v env %v", v, opt, e, e.Env(base))
			}
		}
	}
}
