package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dansailer/sqa/internal/approval"
	"github.com/dansailer/sqa/internal/sonarcli"
)

type env struct {
	t       *testing.T
	home    string
	fixture string
	log     string
}

// newEnv isolates HOME/XDG dirs, copies the fixtures and points sqa at the
// fake sonar script via config.yaml.
func newEnv(t *testing.T) *env {
	t.Helper()
	root, err := filepath.Abs("../../testdata/fake")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	fixture := t.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture, e.Name()), b, 0o755); err != nil { //nolint:gosec // test fixture script
			t.Fatal(err)
		}
	}
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "FAKE_AUTH_EXIT", "FAKE_POST_FAIL", "FAKE_NO_PROJECT"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("sonar:\n  path: "+filepath.Join(fixture, "sonar")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SQA_CONFIG", cfg)
	t.Setenv("FAKE_SONAR_DIR", fixture)
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_SONAR_LOG", log)
	return &env{t: t, home: home, fixture: fixture, log: log}
}

func (e *env) run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func (e *env) calls() []string {
	b, err := os.ReadFile(e.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, strings.ReplaceAll(l, "\x1f", " "))
		}
	}
	return out
}

func (e *env) posts() []string {
	var p []string
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "api post") {
			p = append(p, c)
		}
	}
	return p
}

func TestFindingsRankedSecurityFirst(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("findings", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err, out)
	}
	if res.Project != "demo" {
		t.Errorf("project = %q, want auto-detected demo", res.Project)
	}
	var keys []string
	for _, f := range res.Findings {
		keys = append(keys, f.Key)
	}
	want := "AX-VULN AX-VULN-OLD HS-1 HS-2 AX-BUG AX-SMELL"
	if strings.Join(keys, " ") != want {
		t.Fatalf("order = %v, want %s", keys, want)
	}
	byKey := map[string]bool{}
	for _, f := range res.Findings {
		byKey[f.Key] = f.NewCode
	}
	if !byKey["AX-BUG"] || !byKey["HS-2"] || byKey["AX-VULN"] {
		t.Errorf("new-code flags wrong: %v", byKey)
	}
	if res.Findings[0].File != "internal/api/users.go" || res.Findings[0].Line != 27 {
		t.Errorf("location: %+v", res.Findings[0])
	}
	// hotspots must be fetched through `sonar api get`, for the detected project
	var sawHotspots bool
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "api get /api/hotspots/search") && strings.Contains(c, "project=demo") && strings.Contains(c, "status=TO_REVIEW") {
			sawHotspots = true
		}
		if strings.HasPrefix(c, "integrate") {
			t.Fatal("sonar integrate must never be called")
		}
	}
	if !sawHotspots {
		t.Errorf("no hotspot search call: %v", e.calls())
	}
}

func TestFindingsFiltersAndFailOn(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("findings", "--format", "json", "--security-only", "--project", "demo")
	if code != 0 {
		t.Fatal(code)
	}
	var res FindingsResult
	_ = json.Unmarshal([]byte(out), &res)
	if res.Total != 4 {
		t.Errorf("security-only total %d", res.Total)
	}
	code, out, _ = e.run("findings", "--format", "json", "--min-severity", "CRITICAL")
	_ = json.Unmarshal([]byte(out), &res)
	if code != 0 || res.Total != 4 {
		t.Errorf("min-severity HIGH: code %d total %d", code, res.Total)
	}
	if code, _, _ := e.run("findings", "--fail-on", "BLOCKER"); code != ExitFindings {
		t.Errorf("fail-on exit %d", code)
	}
	if code, _, _ := e.run("findings", "--min-severity", "bogus"); code != ExitUsage {
		t.Errorf("bad severity exit %d", code)
	}
	if code, _, _ := e.run("findings", "--branch", "x", "--pr", "1"); code != ExitUsage {
		t.Errorf("branch+pr exit %d", code)
	}
}

func TestFindingsDefaultFormatIsTOONWhenPiped(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("findings", "--limit", "2")
	if code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out, "findings[2]{rank,key,kind,tier,category,quality,severity,rule,file,line,new_code,status,message}:") {
		t.Fatalf("not TOON:\n%s", out)
	}
	if !strings.Contains(out, "shown: 2") || !strings.Contains(out, "total: 6") {
		t.Fatalf("missing counts:\n%s", out)
	}
}

func TestNext(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("next", "--format", "json")
	if code != 0 {
		t.Fatal(code)
	}
	var n NextResult
	_ = json.Unmarshal([]byte(out), &n)
	if n.Finding == nil || n.Finding.Key != "AX-VULN" || n.Remaining != 6 {
		t.Fatalf("%+v", n)
	}
}

func TestShowIssue(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("show", "AX-VULN", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var b Brief
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatal(err)
	}
	if b.Kind != "issue" || b.Severity != "BLOCKER" || b.Category != "vulnerability" || b.RuleName == "" || b.Language != "Go" {
		t.Errorf("header: %+v", b)
	}
	if !strings.Contains(b.Why, "untrusted data & executes") || !strings.Contains(b.HowToFix, "parameterized") {
		t.Errorf("rule text: why=%q fix=%q", b.Why, b.HowToFix)
	}
	if len(b.Code) != 3 || !b.Code[1].Focus || b.Code[0].Focus || !strings.Contains(b.Code[1].Code, `"SELECT`) || strings.Contains(b.Code[0].Code, "<span") {
		t.Errorf("code: %+v", b.Code)
	}
	if len(b.Flows) != 1 || b.Flows[0].Line != 20 {
		t.Errorf("flows: %+v", b.Flows)
	}
	if !strings.Contains(b.NextStep, "ask for explicit approval") {
		t.Errorf("next step: %s", b.NextStep)
	}
	var sawLines bool
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "api get /api/sources/lines") && strings.Contains(c, "from=22") && strings.Contains(c, "to=33") {
			sawLines = true
		}
	}
	if !sawLines {
		t.Errorf("sources call: %v", e.calls())
	}
}

func TestShowHotspotAndUnknown(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("show", "HS-1", "--format", "json")
	if code != 0 {
		t.Fatal(code)
	}
	var b Brief
	_ = json.Unmarshal([]byte(out), &b)
	if b.Kind != "hotspot" || b.Severity != "HIGH" || b.File != "internal/api/cors.go" {
		t.Errorf("%+v", b)
	}
	if code, _, _ := e.run("show", "NOPE"); code != ExitUsage {
		t.Errorf("unknown key exit %d", code)
	}
}

func TestMarkRequiresApproval(t *testing.T) {
	e := newEnv(t)
	cases := [][]string{
		{"mark", "AX-VULN", "fp", "--comment", "test code only"},
		{"mark", "AX-VULN", "fp", "--approved-by-user"},
		{"mark", "AX-VULN", "fp", "--approved-by-user", "--comment", "  "},
		{"hotspot", "HS-1", "safe", "--comment", "internal only"},
		{"mark", "AX-VULN", "fp", "--comment", "x", "--dry-run"},
	}
	for _, c := range cases {
		code, _, stderr := e.run(c...)
		if code != ExitApproval {
			t.Errorf("%v: exit %d, want 4 (%s)", c, code, stderr)
		}
		if !strings.Contains(stderr, "approval missing") {
			t.Errorf("%v: stderr %q", c, stderr)
		}
	}
	if p := e.posts(); len(p) != 0 {
		t.Fatalf("server was called without approval: %v", p)
	}
	if code, _, _ := e.run("mark", "AX-VULN", "wontfix", "--approved-by-user", "--comment", "x"); code != ExitUsage {
		t.Errorf("bad verdict exit %d", code)
	}
}

func readAudit(t *testing.T, home string) []approval.Entry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".local", "state", "sqa", "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var es []approval.Entry
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e approval.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		es = append(es, e)
	}
	return es
}

func TestMarkApproved(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("mark", "AX-VULN", "fp", "--approved-by-user", "--comment", "Input is validated by middleware", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	p := e.posts()
	if len(p) != 2 {
		t.Fatalf("posts: %v", p)
	}
	if !strings.Contains(p[0], "/api/issues/do_transition") || !strings.Contains(p[0], `"transition":"falsepositive"`) || !strings.Contains(p[0], `"issue":"AX-VULN"`) {
		t.Errorf("transition call: %s", p[0])
	}
	if !strings.Contains(p[1], "/api/issues/add_comment") || !strings.Contains(p[1], `"text":"[sqa] Input is validated by middleware"`) {
		t.Errorf("comment call: %s", p[1])
	}
	var res ChangeResult
	_ = json.Unmarshal([]byte(out), &res)
	if res.Result != "ok" {
		t.Errorf("%+v", res)
	}
	a := readAudit(t, e.home)
	if len(a) != 1 || a[0].Key != "AX-VULN" || a[0].Action != "fp" || a[0].Result != "ok" || a[0].DryRun {
		t.Errorf("audit %+v", a)
	}
}

func TestMarkDryRunAndFailure(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("mark", "AX-VULN", "accept", "--approved-by-user", "--comment", "legacy", "--dry-run")
	if code != 0 || !strings.Contains(out, "dry_run: true") {
		t.Fatalf("exit %d out %s", code, out)
	}
	if len(e.posts()) != 0 {
		t.Fatal("dry run called server")
	}
	t.Setenv("FAKE_POST_FAIL", "1")
	code, _, stderr := e.run("mark", "AX-VULN", "reopen", "--approved-by-user", "--comment", "x")
	if code != ExitServer || !strings.Contains(stderr, "403") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
	a := readAudit(t, e.home)
	if len(a) != 2 || !a[0].DryRun || a[1].Result != "error" {
		t.Errorf("audit %+v", a)
	}
}

func TestHotspotApproved(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := e.run("hotspot", "HS-1", "safe", "--approved-by-user", "--comment", "CORS limited to internal origins")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	p := e.posts()
	if len(p) != 1 || !strings.Contains(p[0], "/api/hotspots/change_status") || !strings.Contains(p[0], `"resolution":"SAFE"`) ||
		!strings.Contains(p[0], `"status":"REVIEWED"`) || !strings.Contains(p[0], `"comment":"[sqa] CORS limited`) {
		t.Fatalf("posts %v", p)
	}
}

func TestDoctor(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("doctor", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s %s", code, out, stderr)
	}
	var d doctorResult
	_ = json.Unmarshal([]byte(out), &d)
	if !d.OK || len(d.Checks) != 3 {
		t.Fatalf("%+v", d)
	}

	if err := os.WriteFile(filepath.Join(e.fixture, "server_version.txt"), []byte("10.7.0.96327\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := e.run("doctor"); code != ExitServer {
		t.Errorf("old server exit %d", code)
	}

	if err := os.WriteFile(filepath.Join(e.fixture, "auth_status.json"), []byte(`{"status":"not_authenticated"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AUTH_EXIT", "1")
	code, out, _ = e.run("doctor")
	if code != ExitServer || !strings.Contains(out, "sqa auth login") {
		t.Errorf("unauthenticated exit %d out %s", code, out)
	}
	if code, _, _ := e.run("auth", "status"); code != ExitServer {
		t.Errorf("auth status exit %d", code)
	}
}

func TestDoctorWithoutSonar(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("sonar:\n  path: /nonexistent/sonar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := e.run("doctor"); code != ExitUsage {
		t.Errorf("exit %d", code)
	}
}

func TestServerVersionSupported(t *testing.T) {
	for v, want := range map[string]bool{
		"2025.1.0.102418": true, "2026.2.0.1": true, "25.1.0.102122": true,
		"10.8.1.101195": false, "9.9.0": false, "garbage": false,
	} {
		if got := ServerVersionSupported(v); got != want {
			t.Errorf("%s: %v", v, got)
		}
	}
}

func TestAuthPassThrough(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("auth", "login", "--server", "https://sonar.example.com")
	if code != 0 || !strings.Contains(out, "auth login passthrough") {
		t.Fatalf("exit %d out %q", code, out)
	}
	calls := e.calls()
	if len(calls) != 1 || calls[0] != "auth login --server https://sonar.example.com" {
		t.Fatalf("calls %v", calls)
	}
}

func TestSkillPaths(t *testing.T) {
	home, cwd := "/home/u", "/repo"
	cases := []struct {
		target  string
		project bool
		want    string
	}{
		{"", false, "/home/u/.agents/skills/sqa/SKILL.md"},
		{"agents", false, "/home/u/.agents/skills/sqa/SKILL.md"},
		{"claude", false, "/home/u/.claude/skills/sqa/SKILL.md"},
		{"opencode", false, "/home/u/.config/opencode/skills/sqa/SKILL.md"},
		{"cursor", false, "/home/u/.cursor/skills/sqa/SKILL.md"},
		{"agents", true, "/repo/.agents/skills/sqa/SKILL.md"},
		{"claude", true, "/repo/.claude/skills/sqa/SKILL.md"},
		{"/opt/skills", false, "/opt/skills/sqa/SKILL.md"},
	}
	for _, c := range cases {
		got, err := SkillPath(c.target, c.project, nil, home, cwd)
		if err != nil || got != filepath.FromSlash(c.want) {
			t.Errorf("%q project=%v: %s %v", c.target, c.project, got, err)
		}
	}
	if _, err := SkillPath("/opt/x", true, nil, home, cwd); err == nil {
		t.Error("path + --project should fail")
	}
}

func TestSkillInstallAndPrint(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := e.run("skill", "install")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	b, err := os.ReadFile(filepath.Join(e.home, ".agents", "skills", "sqa", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "---\nname: sqa\ndescription: ") {
		t.Fatalf("frontmatter: %.80s", b)
	}
	for _, must := range []string{"No MCP", "sonar integrate", "--approved-by-user", "Stop and ask the user explicitly", "Never mark a finding as false positive or accepted to reduce the count"} {
		if !strings.Contains(string(b), must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
	if code, _, _ := e.run("skill", "install", "--target", "claude"); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".claude", "skills", "sqa", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	_, out, _ := e.run("skill", "print")
	if out != string(b) {
		t.Error("print differs from install")
	}
}

func TestForbiddenSonarCommands(t *testing.T) {
	r := &sonarcli.Runner{Bin: "/bin/false"}
	for _, sub := range []string{"integrate", "remediate"} {
		if _, _, err := r.Run(context.Background(), sub, "claude"); err == nil || !strings.Contains(err.Error(), "forbidden") {
			t.Errorf("%s: %v", sub, err)
		}
	}
}

func TestSetupDownloadsAndVerifies(t *testing.T) {
	if _, _, err := sonarcli.Platform(runtime.GOOS, runtime.GOARCH); err != nil {
		t.Skip(err)
	}
	e := newEnv(t)
	// No sonar.path override for setup.
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := []byte("#!/bin/sh\necho 9.9.9\n")
	sum := sha256.Sum256(bin)
	good := hex.EncodeToString(sum[:])
	checksum := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".bin.sha256"):
			_, _ = w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".bin") && strings.Contains(r.URL.Path, "/9.9.9.1/"):
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := distributionBaseURL
	distributionBaseURL = srv.URL
	defer func() { distributionBaseURL = old }()

	checksum = strings.Repeat("0", 64)
	if code, _, stderr := e.run("setup", "--sonar-version", "9.9.9.1"); code != ExitServer || !strings.Contains(stderr, "checksum mismatch") {
		t.Fatalf("mismatch: exit %d %s", code, stderr)
	}
	dest := filepath.Join(e.home, ".local", "share", "sqa", "bin", "sonar")
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("binary installed despite checksum mismatch")
	}
	checksum = good
	code, out, stderr := e.run("setup", "--sonar-version", "9.9.9.1", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if !strings.Contains(out, good) || !strings.Contains(out, `"status": "installed"`) {
		t.Errorf("out %s", out)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("not installed executable: %v", err)
	}
	if code, _, _ := e.run("setup", "--sonar-version", "bad;rm"); code != ExitServer {
		t.Errorf("invalid version exit %d", code)
	}
}

func TestVersion(t *testing.T) {
	e := newEnv(t)
	_, out, _ := e.run("version")
	if !strings.Contains(out, "sonar_cli: "+sonarcli.PinnedVersion) {
		t.Fatal(out)
	}
}

func TestFindingsWithoutProject(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_NO_PROJECT", "1")
	code, _, stderr := e.run("findings")
	if code != ExitUsage || !strings.Contains(stderr, "--project") {
		t.Fatalf("exit %d %s", code, stderr)
	}
}
