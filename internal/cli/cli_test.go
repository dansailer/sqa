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
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "FAKE_AUTH_EXIT", "FAKE_POST_FAIL", "FAKE_NO_PROJECT",
		"FAKE_AUTH_STDERR", "FAKE_WRITE_CREDS", "FAKE_ACK", "FAKE_GATE", "FAKE_NO_NEWCODE", "FAKE_NO_LINEHITS", "SONARQUBE_CLI_KEYCHAIN_FILE", "SONAR_USER_HOME"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	// Tests are independent of the build tag: no bundled sonar unless a test
	// injects one with withBundle.
	old := bundledSonar
	bundledSonar = nil
	t.Cleanup(func() { bundledSonar = old })
	cfg := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("sonar:\n  path: "+filepath.Join(fixture, "sonar")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SQA_CONFIG", cfg)
	t.Setenv("FAKE_SONAR_DIR", fixture)
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_SONAR_LOG", log)
	// Fake gh and git first on PATH so the open-PR check never reaches a real
	// forge. Both fail unless a test sets FAKE_GH_JSON / FAKE_GIT_HEADS.
	forgeBin, err := filepath.Abs("../../testdata/forge")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", forgeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_JSON", "")
	t.Setenv("FAKE_GIT_HEADS", "")
	t.Setenv("FAKE_FORGE_LOG", filepath.Join(t.TempDir(), "forge.log"))
	return &env{t: t, home: home, fixture: fixture, log: log}
}

func (e *env) forgeCalls() []string {
	b, err := os.ReadFile(os.Getenv("FAKE_FORGE_LOG"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func forgeFixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("../../testdata/forge", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
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
	if !strings.Contains(out, "findings[2]{rank,key,kind,tier,category,quality,severity,rule,file,line,new_code,status,message,pr}:") {
		t.Fatalf("not TOON:\n%s", out)
	}
	if !strings.Contains(out, "shown: 2") || !strings.Contains(out, "total: 6") {
		t.Fatalf("missing counts:\n%s", out)
	}
}

func TestNext(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("next", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var n NextResult
	if err := json.Unmarshal([]byte(out), &n); err != nil {
		t.Fatal(err, out)
	}
	if n.Finding == nil || n.Finding.Key != "AX-VULN" || n.Remaining != 6 || n.Project != "demo" {
		t.Fatalf("%+v", n)
	}
	if n.Brief == nil {
		t.Fatal("missing brief")
	}
	if n.Brief.Key != "AX-VULN" || n.Brief.RuleName == "" || n.Brief.Language != "Go" {
		t.Errorf("brief header: %+v", n.Brief)
	}
	if !strings.Contains(n.Brief.Why, "untrusted data & executes") || !strings.Contains(n.Brief.HowToFix, "parameterized") {
		t.Errorf("rule text: why=%q fix=%q", n.Brief.Why, n.Brief.HowToFix)
	}
	if len(n.Brief.Code) != 3 || !n.Brief.Code[1].Focus || !strings.Contains(n.Brief.Code[1].Code, `"SELECT`) {
		t.Errorf("code: %+v", n.Brief.Code)
	}
	if !strings.Contains(n.Hint, "recommended verdict") || strings.Contains(n.Hint, "sqa show") {
		t.Errorf("hint: %s", n.Hint)
	}
	if !strings.Contains(n.Brief.URL, "issues=AX-VULN") || !strings.Contains(n.Brief.URL, "id=demo") {
		t.Errorf("brief url: %s", n.Brief.URL)
	}

	code, out, stderr = e.run("next", "--format", "table")
	if code != 0 {
		t.Fatalf("table exit %d %s", code, stderr)
	}
	for _, must := range []string{"[1 of 6] demo", "untrusted data & executes", "parameterized", `"SELECT`, "vulnerability", "BLOCKER"} {
		if !strings.Contains(out, must) {
			t.Errorf("table missing %q:\n%s", must, out)
		}
	}

	var sawIssue, sawRule, sawLines bool
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "api get /api/issues/search") && strings.Contains(c, "issues=AX-VULN") {
			sawIssue = true
		}
		if strings.HasPrefix(c, "api get /api/rules/show") {
			sawRule = true
		}
		if strings.HasPrefix(c, "api get /api/sources/lines") {
			sawLines = true
		}
	}
	if !sawIssue || !sawRule || !sawLines {
		t.Errorf("brief fetches missing (issue=%v rule=%v lines=%v): %v", sawIssue, sawRule, sawLines, e.calls())
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
	if !strings.Contains(b.URL, "https://sonarqube.example.com/project/issues?") || !strings.Contains(b.URL, "issues=AX-VULN") || !strings.Contains(b.URL, "id=demo") {
		t.Errorf("url: %s", b.URL)
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

func TestFindingURL(t *testing.T) {
	got := FindingURL("https://sonarqube.example.com/", "demo", "AX-VULN", "issue", "", "")
	if !strings.Contains(got, "https://sonarqube.example.com/project/issues?") || !strings.Contains(got, "id=demo") || !strings.Contains(got, "issues=AX-VULN") || !strings.Contains(got, "open=AX-VULN") {
		t.Fatalf("issue: %s", got)
	}
	got = FindingURL("https://sonarqube.example.com", "demo", "HS-1", "hotspot", "main", "")
	if !strings.Contains(got, "/security_hotspots?") || !strings.Contains(got, "hotspots=HS-1") || !strings.Contains(got, "branch=main") || strings.Contains(got, "issues=") {
		t.Fatalf("hotspot branch: %s", got)
	}
	got = FindingURL("https://sonarqube.example.com", "demo", "AX-VULN", "issue", "main", "42")
	if !strings.Contains(got, "pullRequest=42") || strings.Contains(got, "branch=") {
		t.Fatalf("pr wins over branch: %s", got)
	}
	if FindingURL("", "demo", "AX-VULN", "issue", "", "") != "" || FindingURL("https://sq.example", "", "k", "issue", "", "") != "" {
		t.Fatal("empty inputs should yield no URL")
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
	if !strings.Contains(b.URL, "/security_hotspots?") || !strings.Contains(b.URL, "hotspots=HS-1") || !strings.Contains(b.URL, "id=demo") {
		t.Errorf("url: %s", b.URL)
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
	if !d.OK || len(d.Checks) != 6 {
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
	for _, must := range []string{"No MCP", "sonar integrate", "sonar api", "--approved-by-user", "Stop and ask the user explicitly", "Never mark a finding as false positive or accepted to reduce the count", "Conventional Commits", "sonar.tests", "SonarQube URL", "`sonar` label", "already-open PR", "in:body", "sqa next --skip <key>", "pr_check", "sqa coverage --file", "ACKNOWLEDGED", "hotspot <hotspot-key> reopen", "assign <login>", "Writing tests is normal", "sqa pr-body", "every finding the PR resolves", "Grouping needs explicit approval", "Never add an unrelated fix silently", "sqa findings --file '<glob>'"} {
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

// withBundle injects the fake sonar script as the bundled binary and removes
// the sonar.path override, as in a release build.
func (e *env) withBundle() *sonarcli.Bundle {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.fixture, "sonar"))
	if err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	b := &sonarcli.Bundle{Version: sonarcli.PinnedVersion, Platform: "linux-x86-64", SHA256: hex.EncodeToString(sum[:]), Data: data}
	bundledSonar = b
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), nil, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return b
}

func TestBundledSonarIsExtractedOnFirstUse(t *testing.T) {
	e := newEnv(t)
	b := e.withBundle()
	dest := filepath.Join(e.home, ".local", "share", "sqa", "bin", "sonar-"+sonarcli.PinnedVersion)

	code, out, stderr := e.run("version", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var v versionInfo
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err, out)
	}
	if !v.Bundled || v.BundledVersion != sonarcli.PinnedVersion || v.BundledSHA256 != b.SHA256 || v.SonarSource != "bundled" || v.SonarPath != dest {
		t.Fatalf("%+v", v)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("version must not extract")
	}

	code, out, stderr = e.run("doctor", "--format", "json")
	if code != 0 {
		t.Fatalf("doctor exit %d %s %s", code, out, stderr)
	}
	if !strings.Contains(out, "bundled in sqa") {
		t.Errorf("doctor does not report the bundled source: %s", out)
	}
	if st, err := os.Stat(dest); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("not extracted: %v", err)
	}
	if code, out, _ := e.run("findings", "--format", "json"); code != 0 || !strings.Contains(out, "AX-VULN") {
		t.Fatalf("findings via bundled sonar: %d %s", code, out)
	}
}

func TestSetupWithBundledSonar(t *testing.T) {
	e := newEnv(t)
	b := e.withBundle()
	code, out, stderr := e.run("setup", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if !strings.Contains(out, `"status": "extracted"`) || !strings.Contains(out, `"source": "bundled"`) || !strings.Contains(out, b.SHA256) {
		t.Fatalf("%s", out)
	}
	if _, out, _ = e.run("setup", "--format", "json"); !strings.Contains(out, `"status": "already extracted"`) {
		t.Fatalf("%s", out)
	}
	if _, out, _ = e.run("setup", "--force", "--format", "json"); !strings.Contains(out, `"status": "extracted"`) {
		t.Fatalf("force: %s", out)
	}

	// A corrupt bundle never reaches disk or exec.
	b.Data = append([]byte(nil), b.Data...)
	b.Data[len(b.Data)-2] ^= 1
	if code, _, stderr := e.run("setup", "--force"); code != ExitUsage || !strings.Contains(stderr, "corrupt") {
		t.Fatalf("corrupt bundle: exit %d %s", code, stderr)
	}
}

func TestSonarVersionOverrideBypassesBundle(t *testing.T) {
	e := newEnv(t)
	e.withBundle()
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), []byte("sonar:\n  version: \"9.9.9.1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	code, out, _ := e.run("version", "--format", "json")
	if code != 0 || !strings.Contains(out, `"sonar_source": "not found"`) {
		t.Fatalf("override must not use the bundle: %d %s", code, out)
	}
}

// envLog points the fake sonar at a file where it records the token-store
// variables it received, one "cmd|SONARQUBE_CLI_KEYCHAIN_FILE|SONAR_USER_HOME"
// line per call.
func (e *env) envLog() string {
	p := filepath.Join(e.t.TempDir(), "env.log")
	e.t.Setenv("FAKE_SONAR_ENVLOG", p)
	return p
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func permOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestFileTokenStoreIsDefault(t *testing.T) {
	e := newEnv(t)
	log := e.envLog()
	t.Setenv("FAKE_WRITE_CREDS", "1")
	if code, out, stderr := e.run("auth", "login", "--server", "https://sonar.example.com"); code != 0 {
		t.Fatalf("exit %d %s %s", code, out, stderr)
	}
	creds := filepath.Join(e.home, ".config", "sqa", "credentials.json")
	home := filepath.Join(e.home, ".local", "share", "sqa", "sonar-home")
	if got := readLines(t, log); len(got) != 1 || got[0] != "auth|"+creds+"|"+home {
		t.Fatalf("sonar env %v", got)
	}
	// The fake replaced the file with mode 0644; sqa must restore 0600.
	if m := permOf(t, creds); m != 0o600 {
		t.Errorf("credentials.json %04o", m)
	}
	if m := permOf(t, filepath.Dir(creds)); m != 0o700 {
		t.Errorf("config dir %04o", m)
	}
	if m := permOf(t, home); m != 0o700 {
		t.Errorf("sonar home %04o", m)
	}

	code, out, _ := e.run("auth", "status", "--format", "json")
	var st authStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil || code != 0 {
		t.Fatalf("exit %d %s", code, out)
	}
	if st.Source != "credentials file" || st.TokenStore != "file "+creds || st.SonarHome != home {
		t.Errorf("auth status %+v", st)
	}
}

func TestTokenStoreOverrides(t *testing.T) {
	e := newEnv(t)
	log := e.envLog()
	dir := t.TempDir()
	cfg := filepath.Join(e.home, "config.yaml")
	data := "sonar:\n  path: " + filepath.Join(e.fixture, "sonar") + "\n  home: " + filepath.Join(dir, "sh") +
		"\nauth:\n  credentials_file: " + filepath.Join(dir, "c.json") + "\n"
	if err := os.WriteFile(cfg, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	e.run("auth", "status")
	t.Setenv("SONARQUBE_CLI_KEYCHAIN_FILE", "/env/c.json")
	t.Setenv("SONAR_USER_HOME", "/env/home")
	e.run("auth", "status")
	t.Setenv("SONARQUBE_CLI_KEYCHAIN_FILE", "")
	t.Setenv("SONAR_USER_HOME", "")
	if err := os.WriteFile(cfg, []byte("sonar:\n  path: "+filepath.Join(e.fixture, "sonar")+"\nauth:\n  store: keychain\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("auth", "status", "--format", "table")
	if code != 0 || !strings.Contains(out, "token store: OS keychain") || !strings.Contains(out, "source: OS Keychain") {
		t.Errorf("keychain status %d %s", code, out)
	}
	got := readLines(t, log)
	want := []string{
		"auth|" + filepath.Join(dir, "c.json") + "|" + filepath.Join(dir, "sh"),
		"auth|/env/c.json|/env/home",
		"auth||" + filepath.Join(e.home, ".local", "share", "sqa", "sonar-home"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sonar env\n got %v\nwant %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".config", "sqa", "credentials.json")); !os.IsNotExist(err) {
		t.Error("default credentials file created although never selected")
	}
}

func TestBadStoreConfig(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("auth:\n  store: plaintext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := e.run("auth", "status"); code != ExitUsage || !strings.Contains(stderr, "auth.store") {
		t.Fatalf("exit %d %s", code, stderr)
	}
}

func TestDoctorTokenStore(t *testing.T) {
	e := newEnv(t)
	creds := filepath.Join(e.home, ".config", "sqa", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(creds), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(creds), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(creds, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("doctor", "--format", "json")
	var d doctorResult
	_ = json.Unmarshal([]byte(out), &d)
	if code != 0 || d.Checks[0].Name != "token store" || d.Checks[0].Status != "warn" ||
		!strings.Contains(d.Checks[0].Detail, "directory was 0755") || !strings.Contains(d.Checks[0].Detail, "file was 0644") {
		t.Fatalf("exit %d %+v", code, d)
	}
	if permOf(t, creds) != 0o600 || permOf(t, filepath.Dir(creds)) != 0o700 {
		t.Error("doctor did not fix permissions")
	}
	if !strings.Contains(out, "via credentials file") {
		t.Errorf("auth source not relabelled: %s", out)
	}
	_, out, _ = e.run("doctor", "--format", "json")
	_ = json.Unmarshal([]byte(out), &d)
	if d.Checks[0].Status != "ok" || !strings.HasSuffix(d.Checks[0].Detail, "(0600)") {
		t.Errorf("second run %+v", d.Checks[0])
	}
}

func TestDoctorKeychainHints(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_AUTH_STDERR", "Error: No usable system keychain is available: libsecret could not be loaded.")
	// File store: the keychain hint must not appear.
	_, out, _ := e.run("doctor")
	if strings.Contains(out, "apt install") {
		t.Errorf("keychain hint with file store: %s", out)
	}
	cfg := filepath.Join(e.home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("sonar:\n  path: "+filepath.Join(e.fixture, "sonar")+"\nauth:\n  store: keychain\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("doctor")
	if code != ExitServer || !strings.Contains(out, "libsecret-1-0 gnome-keyring") {
		t.Errorf("libsecret hint: %d %s", code, out)
	}
	t.Setenv("FAKE_AUTH_STDERR", "Failed to access the system keychain. Underlying error: The name org.freedesktop.secrets was not provided by any .service files")
	if _, out, _ := e.run("doctor"); !strings.Contains(out, "gnome-keyring-daemon --unlock") {
		t.Errorf("secret service hint: %s", out)
	}
}

func TestTelemetryShownAndPassed(t *testing.T) {
	e := newEnv(t)
	t.Setenv("DO_NOT_TRACK", "") // registers the restore; the variable must be unset, not empty
	os.Unsetenv("DO_NOT_TRACK")
	p := filepath.Join(t.TempDir(), "dnt.log")
	script := filepath.Join(t.TempDir(), "sonar")
	fake := filepath.Join(e.fixture, "sonar")
	body := "#!/bin/sh\nprintf '%s|%s\\n' \"$1\" \"${DO_NOT_TRACK-<unset>}\" >>" + p + "\nexec " + fake + " \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(e.home, "config.yaml")
	write := func(extra string) {
		if err := os.WriteFile(cfg, []byte("sonar:\n  path: "+script+"\n"+extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	_, out, _ := e.run("doctor", "--format", "table")
	if !strings.Contains(out, "telemetry    off (sqa sets DO_NOT_TRACK=1") {
		t.Errorf("doctor: %s", out)
	}
	_, out, _ = e.run("version", "--format", "table")
	if !strings.Contains(out, "sonar telemetry:    off") {
		t.Errorf("version: %s", out)
	}
	write("  telemetry: true\n")
	_, out, _ = e.run("doctor", "--format", "table")
	if !strings.Contains(out, "on (sonar.telemetry: true)") {
		t.Errorf("opt-in doctor: %s", out)
	}
	write("")
	t.Setenv("DO_NOT_TRACK", "0")
	if code, out, _ := e.run("doctor", "--format", "table"); code != 0 || !strings.Contains(out, `on (DO_NOT_TRACK="0" from your environment)`) {
		t.Errorf("env 0 doctor: %s", out)
	}
	got := strings.Join(readLines(t, p), "\n")
	for _, want := range []string{"auth|1", "auth|<unset>", "auth|0"} {
		if !strings.Contains(got, want) {
			t.Errorf("sonar saw DO_NOT_TRACK %q? log:\n%s", want, got)
		}
	}
}

func runNext(t *testing.T, e *env, args ...string) NextResult {
	t.Helper()
	code, out, stderr := e.run(append([]string{"next", "--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var n NextResult
	if err := json.Unmarshal([]byte(out), &n); err != nil {
		t.Fatal(err, out)
	}
	return n
}

func TestFindingsPRColumn(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_GH_JSON", forgeFixture(t, "prs.json"))
	code, out, stderr := e.run("findings", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err, out)
	}
	prs := map[string]string{}
	for _, f := range res.Findings {
		prs[f.Key] = f.PR
	}
	want := map[string]string{
		"AX-VULN":     "https://github.com/acme/demo/pull/12", // key in the PR body
		"HS-1":        "https://github.com/acme/demo/pull/15", // key in the head branch
		"AX-VULN-OLD": "", "HS-2": "", "AX-BUG": "", "AX-SMELL": "",
	}
	for k, v := range want {
		if prs[k] != v {
			t.Errorf("%s: pr = %q, want %q", k, prs[k], v)
		}
	}
	if res.PRCheck != "gh" || res.Note != "" {
		t.Errorf("pr_check=%q note=%q", res.PRCheck, res.Note)
	}
	if c := e.forgeCalls(); len(c) != 1 || !strings.HasPrefix(c[0], "gh pr list --state open") {
		t.Errorf("want one gh call per run, got %v", c)
	}

	code, out, _ = e.run("findings", "--format", "table")
	if code != 0 || !strings.Contains(out, " PR ") || !strings.Contains(out, "#12") || !strings.Contains(out, "#15") {
		t.Errorf("table:\n%s", out)
	}
	code, out, _ = e.run("findings", "--format", "md")
	if code != 0 || !strings.Contains(out, "| new | PR | message |") || !strings.Contains(out, "pull/12") {
		t.Errorf("md:\n%s", out)
	}
	code, out, _ = e.run("findings", "--format", "toon")
	if code != 0 || !strings.Contains(out, "pr}:") || !strings.Contains(out, "pull/12") {
		t.Errorf("toon must stay tabular with a pr field:\n%s", out)
	}
}

func TestFindingsPRCheckUnavailableAndOff(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("findings", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.PRCheck != "unavailable" || !strings.Contains(res.Note, "gh auth login") || !strings.Contains(res.Note, "not a git repository") {
		t.Errorf("pr_check=%q note=%q", res.PRCheck, res.Note)
	}
	for _, f := range res.Findings {
		if f.PR != "" {
			t.Errorf("%s: unexpected pr %q", f.Key, f.PR)
		}
	}
	if len(res.Findings) != 6 {
		t.Errorf("findings must still be listed: %d", len(res.Findings))
	}

	t.Setenv("FAKE_FORGE_LOG", filepath.Join(t.TempDir(), "off.log"))
	t.Setenv("FAKE_GH_JSON", forgeFixture(t, "prs.json"))
	code, out, _ = e.run("findings", "--format", "json", "--no-pr-check")
	if code != 0 {
		t.Fatal(out)
	}
	res = FindingsResult{}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.PRCheck != "off" || res.Findings[0].PR != "" || e.forgeCalls() != nil {
		t.Errorf("--no-pr-check: pr_check=%q pr=%q calls=%v", res.PRCheck, res.Findings[0].PR, e.forgeCalls())
	}
}

func TestNextSkipsFindingsWithOpenPR(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_GH_JSON", forgeFixture(t, "prs.json"))
	n := runNext(t, e)
	if n.Finding == nil || n.Finding.Key != "AX-VULN-OLD" || n.Finding.Rank != 2 || n.Finding.PR != "" {
		t.Fatalf("next = %+v", n.Finding)
	}
	if n.Total != 6 || n.Remaining != 4 || n.PRCheck != "gh" {
		t.Errorf("total=%d remaining=%d pr_check=%q", n.Total, n.Remaining, n.PRCheck)
	}
	if len(n.Skipped) != 2 || n.Skipped[0].Key != "AX-VULN" || n.Skipped[0].Reason != "open PR" || !strings.HasSuffix(n.Skipped[0].PR, "/pull/12") ||
		n.Skipped[1].Key != "HS-1" {
		t.Errorf("skipped: %+v", n.Skipped)
	}
	if n.Brief == nil || n.Brief.Key != "AX-VULN-OLD" {
		t.Errorf("brief: %+v", n.Brief)
	}
	if c := e.forgeCalls(); len(c) != 1 {
		t.Errorf("want one forge call, got %v", c)
	}

	code, out, _ := e.run("next", "--format", "table")
	if code != 0 || !strings.Contains(out, "Skipped 2: AX-VULN (open PR https://github.com/acme/demo/pull/12)") || !strings.Contains(out, "[2 of 6] demo") {
		t.Errorf("table:\n%s", out)
	}

	n = runNext(t, e, "--include-open-pr")
	if n.Finding == nil || n.Finding.Key != "AX-VULN" || !strings.HasSuffix(n.Finding.PR, "/pull/12") || len(n.Skipped) != 0 {
		t.Fatalf("--include-open-pr: %+v skipped=%+v", n.Finding, n.Skipped)
	}
	if n.Brief == nil || !strings.HasSuffix(n.Brief.PR, "/pull/12") || !strings.Contains(n.Hint, "already has an open fix PR") {
		t.Errorf("brief pr=%v hint=%q", n.Brief, n.Hint)
	}
}

func TestNextManualSkip(t *testing.T) {
	e := newEnv(t)
	n := runNext(t, e, "--skip", "AX-VULN", "--skip", "AX-VULN-OLD,HS-2")
	if n.Finding == nil || n.Finding.Key != "HS-1" {
		t.Fatalf("next = %+v", n.Finding)
	}
	var keys []string
	for _, sk := range n.Skipped {
		if sk.Reason != "--skip" {
			t.Errorf("reason: %+v", sk)
		}
		keys = append(keys, sk.Key)
	}
	if strings.Join(keys, " ") != "AX-VULN AX-VULN-OLD HS-2" || n.Remaining != 3 {
		t.Errorf("skipped=%v remaining=%d", keys, n.Remaining)
	}
	if n.PRCheck != "unavailable" || !strings.Contains(n.Note, "open-PR check unavailable") {
		t.Errorf("degraded check must be reported: %q %q", n.PRCheck, n.Note)
	}

	n = runNext(t, e, "--skip", "AX-VULN,AX-VULN-OLD,HS-1,HS-2,AX-BUG,AX-SMELL")
	if n.Finding != nil || n.Remaining != 0 || len(n.Skipped) != 6 {
		t.Fatalf("all skipped: %+v", n)
	}
	code, out, _ := e.run("next", "--format", "table", "--skip", "AX-VULN,AX-VULN-OLD,HS-1,HS-2,AX-BUG,AX-SMELL")
	if code != 0 || !strings.Contains(out, "No other open findings in demo.") {
		t.Errorf("table:\n%s", out)
	}
}

func TestNextGitBranchFallback(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_GIT_HEADS", forgeFixture(t, "heads.txt"))
	n := runNext(t, e)
	if n.Finding == nil || n.Finding.Key != "AX-VULN-OLD" {
		t.Fatalf("next = %+v", n.Finding)
	}
	if len(n.Skipped) != 1 || n.Skipped[0].PR != "branch:sonar/go-s3649-ax-vuln" || n.PRCheck != "git" ||
		!strings.Contains(n.Note, "remote sonar/* branches") {
		t.Errorf("skipped=%+v check=%q note=%q", n.Skipped, n.PRCheck, n.Note)
	}
	c := e.forgeCalls()
	if len(c) != 2 || !strings.HasPrefix(c[0], "gh pr list") || c[1] != "git ls-remote --heads origin refs/heads/sonar/*" {
		t.Errorf("calls: %v", c)
	}
}

func TestShowReportsOpenPR(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_GH_JSON", forgeFixture(t, "prs.json"))
	code, out, stderr := e.run("show", "AX-VULN", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var b Brief
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(b.PR, "/pull/12") || b.PRCheck != "gh" {
		t.Errorf("pr=%q check=%q", b.PR, b.PRCheck)
	}
	code, out, _ = e.run("show", "AX-VULN", "--format", "md")
	if code != 0 || !strings.Contains(out, "**Open fix PR:** https://github.com/acme/demo/pull/12") {
		t.Errorf("md:\n%s", out)
	}
}
