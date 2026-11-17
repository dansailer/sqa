package sonarcli

import (
	"path/filepath"
	"slices"
	"strings"
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
