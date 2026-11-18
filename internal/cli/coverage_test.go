package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func runCoverage(t *testing.T, e *env, args ...string) CoverageResult {
	t.Helper()
	code, out, stderr := e.run(append([]string{"coverage", "--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var r CoverageResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	return r
}

func (e *env) callsMatching(prefix string) []string {
	var out []string
	for _, c := range e.calls() {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestCoverageRanksFilesByUncoveredNewLines(t *testing.T) {
	e := newEnv(t)
	r := runCoverage(t, e)
	if r.Project != "demo" || r.Scope != "new_code" || r.Coverage == nil || *r.Coverage != 62.5 ||
		r.LinesToCover != 40 || r.UncoveredLines != 12 || r.UncoveredConditions != 3 {
		t.Fatalf("header: %+v", r)
	}
	if r.Gate == nil || r.Gate.Metric != "new_coverage" || r.Gate.Threshold != "80" || r.Gate.Status != "ERROR" {
		t.Errorf("gate: %+v", r.Gate)
	}
	// Fully covered files are dropped; ranks follow the server's worst-first order.
	if r.TotalFiles != 3 || len(r.Files) != 2 {
		t.Fatalf("files: total=%d %+v", r.TotalFiles, r.Files)
	}
	f := r.Files[0]
	if f.Rank != 1 || f.File != "internal/api/users.go" || f.Coverage != 40 || f.UncoveredLines != 6 || f.UncoveredConditions != 1 || f.LinesToCover != 10 {
		t.Errorf("first file: %+v", f)
	}
	if r.Files[1].Rank != 2 || r.Files[1].File != "internal/db/query.go" {
		t.Errorf("second file: %+v", r.Files[1])
	}
	if !strings.Contains(r.Hint, "ask the user before branching") {
		t.Errorf("hint: %s", r.Hint)
	}
	tree := e.callsMatching("api get /api/measures/component_tree")
	if len(tree) != 1 {
		t.Fatalf("component_tree calls: %v", e.calls())
	}
	for _, must := range []string{"component=demo&", "metricSort=new_uncovered_lines", "metricPeriodSort=1", "&s=metricPeriod", "strategy=leaves", "qualifiers=FIL", "asc=false", "ps=20"} {
		if !strings.Contains(tree[0], must) {
			t.Errorf("component_tree call missing %q: %s", must, tree[0])
		}
	}
	// Project auto-detection costs one `sonar list issues --page-size 1`.
	if l := e.callsMatching("list issues"); len(l) != 1 || !strings.Contains(l[0], "--page-size 1") {
		t.Errorf("list calls: %v", l)
	}
	if len(e.posts()) != 0 {
		t.Error("coverage must be read-only")
	}
	for _, c := range e.calls() {
		if !strings.HasPrefix(c, "api get") && !strings.HasPrefix(c, "list issues") {
			t.Errorf("unexpected sonar call: %s", c)
		}
	}
}

func TestCoverageOutputs(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("coverage", "--format", "table", "--project", "demo")
	if code != 0 || !strings.Contains(out, "Coverage on new code: 62.5% (12 of 40 lines uncovered, 3 uncovered conditions); gate new_coverage >= 80% (failing)") ||
		!strings.Contains(out, "internal/api/users.go") || strings.Contains(out, "strings.go") {
		t.Errorf("table:\n%s", out)
	}
	code, out, _ = e.run("coverage", "--format", "md")
	if code != 0 || !strings.Contains(out, "| 1 | internal/api/users.go | 40.0% | 6 | 1 | 10 |") || !strings.Contains(out, "Showing 2 of 3 files") {
		t.Errorf("md:\n%s", out)
	}
	code, out, _ = e.run("coverage")
	if code != 0 || !strings.Contains(out, "files[2]{rank,file,coverage,uncovered_lines,uncovered_conditions,lines_to_cover}:") {
		t.Errorf("toon:\n%s", out)
	}
}

func TestCoverageOverall(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_NO_NEWCODE", "1")
	r := runCoverage(t, e)
	if r.Scope != "overall" || *r.Coverage != 71.3 || r.UncoveredLines != 300 || !strings.Contains(r.Note, "no new-code coverage") {
		t.Fatalf("fallback: %+v", r)
	}
	if len(r.Files) != 2 || r.Files[0].File != "internal/legacy/old.go" || r.Files[0].UncoveredLines != 180 {
		t.Errorf("files: %+v", r.Files)
	}
	tree := e.callsMatching("api get /api/measures/component_tree")
	if len(tree) != 1 || !strings.Contains(tree[0], "metricSort=uncovered_lines") || !strings.Contains(tree[0], "&s=metric&") || strings.Contains(tree[0], "metricPeriodSort") {
		t.Errorf("overall sort: %v", tree)
	}

	t.Setenv("FAKE_NO_NEWCODE", "")
	r = runCoverage(t, e, "--overall", "--top", "5")
	if r.Scope != "overall" || r.Note != "" || *r.Coverage != 71.3 {
		t.Errorf("--overall: %+v", r)
	}
	if tree := e.callsMatching("api get /api/measures/component_tree"); !strings.Contains(tree[len(tree)-1], "ps=5") {
		t.Errorf("--top: %v", tree)
	}
}

func TestCoverageFileDetail(t *testing.T) {
	e := newEnv(t)
	r := runCoverage(t, e, "--file", "internal/api/users.go")
	if r.Detail == nil || r.Component != "internal/api/users.go" || *r.Coverage != 40 || len(r.Files) != 0 {
		t.Fatalf("detail: %+v", r)
	}
	d := r.Detail
	// New code only: lines 20 and 29 are old; 24 has no lineHits (not executable).
	if len(d.Uncovered) != 2 || d.Uncovered[0] != (LineRange{23, 23}) || d.Uncovered[1] != (LineRange{25, 26}) {
		t.Errorf("uncovered: %+v", d.Uncovered)
	}
	if len(d.Partial) != 2 || d.Partial[0] != (PartialLine{22, 1, 2}) || d.Partial[1] != (PartialLine{28, 0, 2}) {
		t.Errorf("partial: %+v", d.Partial)
	}
	if d.Note != "" {
		t.Errorf("note: %s", d.Note)
	}
	src := e.callsMatching("api get /api/sources/lines")
	if len(src) != 1 || !strings.Contains(src[0], "key=demo%3Ainternal%2Fapi%2Fusers.go") || strings.Contains(src[0], "from=") {
		t.Errorf("sources call: %v", src)
	}
	if len(e.callsMatching("api get /api/measures/component_tree")) != 0 {
		t.Error("a file needs no component_tree call")
	}

	r = runCoverage(t, e, "--file", "internal/api/users.go", "--overall")
	if got := r.Detail.Uncovered; len(got) != 4 || got[0] != (LineRange{20, 20}) || got[3] != (LineRange{29, 29}) {
		t.Errorf("overall ranges: %+v", got)
	}

	code, out, _ := e.run("coverage", "--file", "internal/api/users.go", "--format", "table")
	if code != 0 || !strings.Contains(out, "Uncovered lines: 23, 25-26") || !strings.Contains(out, "22 (1/2), 28 (0/2)") {
		t.Errorf("table:\n%s", out)
	}
}

func TestCoverageFileWithoutLineHits(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_NO_LINEHITS", "1")
	r := runCoverage(t, e, "--file", "internal/api/users.go")
	if r.Detail == nil || len(r.Detail.Uncovered) != 0 || len(r.Detail.Partial) != 0 || !strings.Contains(r.Detail.Note, "no line coverage (lineHits)") {
		t.Fatalf("detail: %+v", r.Detail)
	}
	code, out, _ := e.run("coverage", "--file", "internal/api/users.go", "--format", "md")
	if code != 0 || !strings.Contains(out, "no line coverage") || strings.Contains(out, "No uncovered lines") {
		t.Errorf("md:\n%s", out)
	}
}

func TestCoverageDirectoryAndUnknownPath(t *testing.T) {
	e := newEnv(t)
	r := runCoverage(t, e, "--file", "internal/api/")
	if r.Detail != nil || r.Component != "internal/api" || *r.Coverage != 50 || len(r.Files) != 2 {
		t.Fatalf("dir: %+v", r)
	}
	if tree := e.callsMatching("api get /api/measures/component_tree"); len(tree) != 1 || !strings.Contains(tree[0], "component=demo%3Ainternal%2Fapi&") {
		t.Errorf("dir tree: %v", tree)
	}
	code, _, stderr := e.run("coverage", "--file", "nope/missing.go")
	if code != ExitUsage || !strings.Contains(stderr, `no file or directory "nope/missing.go"`) {
		t.Errorf("unknown path: exit %d %s", code, stderr)
	}
}

func TestCoverageFailOnAndFlags(t *testing.T) {
	e := newEnv(t)
	if code, _, _ := e.run("coverage", "--fail-on", "70"); code != ExitFindings {
		t.Errorf("62.5 < 70: exit %d, want 1", code)
	}
	if code, _, stderr := e.run("coverage", "--fail-on", "60%"); code != 0 {
		t.Errorf("62.5 >= 60: exit %d %s", code, stderr)
	}
	for _, bad := range [][]string{{"--fail-on", "abc"}, {"--fail-on", "120"}, {"--top", "-1"}, {"--top", "501"}, {"--branch", "x", "--pr", "1"}} {
		if code, _, _ := e.run(append([]string{"coverage"}, bad...)...); code != ExitUsage {
			t.Errorf("%v: exit %d, want 2", bad, code)
		}
	}
	t.Setenv("FAKE_NO_PROJECT", "1")
	if code, _, stderr := e.run("coverage"); code != ExitUsage || !strings.Contains(stderr, "could not determine the project key") {
		t.Errorf("no project: exit %d %s", code, stderr)
	}
	// An explicit project and branch need no auto-detection call.
	e2 := newEnv(t)
	runCoverage(t, e2, "--project", "demo", "--branch", "main")
	if l := e2.callsMatching("list issues"); len(l) != 0 {
		t.Errorf("unexpected detection: %v", l)
	}
	if tree := e2.callsMatching("api get /api/measures/component_tree"); len(tree) != 1 || !strings.Contains(tree[0], "branch=main") {
		t.Errorf("branch not passed: %v", tree)
	}
}

func TestFindingsGateSummary(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("findings", "--format", "json", "--no-pr-check")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	s := res.Summary
	if s == nil || s.Status != "ERROR" || s.Coverage == nil || s.Coverage.Metric != "new_coverage" || s.Coverage.Actual != "62.5" || s.Coverage.Status != "ERROR" {
		t.Fatalf("summary: %+v", s)
	}
	if s.HotspotsReviewed == nil || s.HotspotsReviewed.Metric != "new_security_hotspots_reviewed" || s.HotspotsReviewed.Threshold != "100" {
		t.Errorf("hotspots reviewed: %+v", s.HotspotsReviewed)
	}
	if n := len(e.callsMatching("api get /api/qualitygates/project_status")); n != 1 || len(e.callsMatching("api get /api/measures")) != 0 {
		t.Errorf("want exactly one gate call and no measures call: %v", e.calls())
	}

	code, out, _ = e.run("findings", "--format", "table", "--no-pr-check")
	for _, must := range []string{
		"Coverage on new code: 62.5% (gate requires >= 80%: failing). Run `sqa coverage` for the files that need tests.",
		"Security hotspots reviewed on new code: 50.0% (gate requires >= 100%: failing).",
	} {
		if code != 0 || !strings.Contains(out, must) {
			t.Errorf("table missing %q:\n%s", must, out)
		}
	}
	code, out, _ = e.run("findings", "--format", "md", "--no-pr-check")
	if code != 0 || !strings.Contains(out, "Run `sqa coverage`") {
		t.Errorf("md:\n%s", out)
	}
	code, out, _ = e.run("findings", "--no-pr-check")
	if code != 0 || !strings.Contains(out, "summary:") || !strings.Contains(out, "hotspots_reviewed:") {
		t.Errorf("toon:\n%s", out)
	}

	e2 := newEnv(t)
	code, out, _ = e2.run("findings", "--format", "json", "--no-summary", "--no-pr-check")
	if code != 0 || strings.Contains(out, `"summary"`) || len(e2.callsMatching("api get /api/qualitygates")) != 0 {
		t.Errorf("--no-summary: %s %v", out, e2.calls())
	}
	// next carries no summary and makes no gate call.
	e3 := newEnv(t)
	if code, _, _ := e3.run("next", "--format", "json", "--no-pr-check"); code != 0 || len(e3.callsMatching("api get /api/qualitygates")) != 0 {
		t.Errorf("next made a gate call: %v", e3.calls())
	}
}

func TestFindingsGateSummaryFallbacks(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_GATE", "nocov")
	code, out, _ := e.run("findings", "--format", "json", "--no-pr-check")
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 {
		t.Fatal(err, code)
	}
	s := res.Summary
	if s.Coverage == nil || s.Coverage.Actual != "62.5" || s.Coverage.Comparator != "" || s.HotspotsReviewed != nil {
		t.Fatalf("no gate condition: %+v", s)
	}
	if !strings.Contains(strings.Join(s.Lines(), "\n"), "Coverage on new code: 62.5% (no condition in the quality gate)") {
		t.Errorf("lines: %v", s.Lines())
	}
	if len(e.callsMatching("api get /api/measures/component")) != 1 {
		t.Errorf("want one measures call: %v", e.calls())
	}

	t.Setenv("FAKE_GATE", "fail")
	code, out, stderr := e.run("findings", "--format", "json", "--no-pr-check")
	if code != 0 {
		t.Fatalf("gate failure must not fail findings: %d %s", code, stderr)
	}
	res = FindingsResult{}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Summary.Note, "quality gate unavailable") || res.Summary.Coverage == nil || len(res.Findings) != 6 {
		t.Errorf("summary: %+v", res.Summary)
	}
}
