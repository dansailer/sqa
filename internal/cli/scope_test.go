package cli

import (
	"strings"
	"testing"
)

// Without --branch/--pr every command reads the main branch: sonar would
// otherwise pick a pull request from the local git branch, so sqa passes the
// main branch by name to `sonar list issues` and nothing else gets a PR.
func TestNoBranchOrPRMeansMainBranchEverywhere(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_DETECT_PR", "42")
	n := runNext(t, e, "--no-pr-check")
	if n.Finding == nil || n.Brief == nil {
		t.Fatalf("no brief: %+v", n)
	}
	for _, args := range [][]string{
		{"findings", "--no-pr-check"},
		{"pr-body", "--from-findings"},
		{"coverage"},
		{"show", "AX-VULN", "--no-pr-check"},
	} {
		if code, _, stderr := e.run(append(args, "--format", "json")...); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, stderr)
		}
	}
	lists := 0
	for _, c := range e.calls() {
		if strings.Contains(c, "pullRequest") || strings.Contains(c, "--pull-request") {
			t.Errorf("auto-detected PR leaked into: %s", c)
		}
		if strings.HasPrefix(c, "list issues") {
			lists++
			if !strings.Contains(c, "--branch main") && !strings.Contains(c, "--page-size 1 ") {
				t.Errorf("list issues without the main branch: %s", c)
			}
		}
	}
	if lists == 0 {
		t.Fatal("no list issues call")
	}
	if strings.Contains(n.Brief.URL, "branch=") || strings.Contains(n.Brief.URL, "pullRequest") {
		t.Errorf("main-branch link must not name a branch: %s", n.Brief.URL)
	}
}

func TestExplicitBranchAndPRArePassedThrough(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := e.run("findings", "--pr", "7", "--no-pr-check", "--format", "json"); code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	for _, c := range e.calls() {
		if strings.Contains(c, "project_branches") {
			t.Errorf("no main-branch lookup needed with --pr: %s", c)
		}
		if strings.HasPrefix(c, "list issues") && !strings.Contains(c, "--pull-request 7") {
			t.Errorf("--pr not passed: %s", c)
		}
	}
}

func TestShowLooksUpTheIssueOnTheGivenBranch(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := e.run("show", "AX-VULN", "--branch", "feature/x", "--no-pr-check", "--format", "json"); code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	for _, c := range e.calls() {
		if strings.Contains(c, "/api/issues/search?") && !strings.Contains(c, "branch=feature%2Fx") {
			t.Errorf("issue lookup ignores --branch: %s", c)
		}
	}
}

func TestTriageRejectsPrefixOnlyComment(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"mark", "AX-VULN", "fp", "--approved-by-user", "--comment", "[sqa]"},
		{"hotspot", "HS-1", "safe", "--approved-by-user", "--comment", " [sqa] "},
	} {
		if code, _, _ := e.run(args...); code != ExitApproval {
			t.Errorf("%v: exit %d, want %d", args, code, ExitApproval)
		}
	}
	if len(e.posts()) != 0 {
		t.Errorf("posted without a justification: %v", e.posts())
	}
}

// Only an HTTP 404 means "no such key"; a server error that happens to
// mention 404 is a server error (exit 3).
func TestShowServerErrorMentioning404IsNotNotFound(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := e.run("show", "HS-500", "--no-pr-check")
	if code != ExitServer || strings.Contains(stderr, "no issue or hotspot") {
		t.Errorf("exit %d, want %d: %s", code, ExitServer, stderr)
	}
	if code, _, _ := e.run("show", "NOPE", "--no-pr-check"); code != ExitUsage {
		t.Errorf("unknown key: exit %d, want %d", code, ExitUsage)
	}
}

// "./dir/file", "dir/" and similar shell-completed paths reach sonar in
// SonarQube's form, as `sqa coverage --file` already did.
func TestFindingsFilePathIsNormalised(t *testing.T) {
	e := newEnv(t)
	for _, in := range []string{"./internal/api/", "internal/api/", "/internal/api"} {
		if code, _, stderr := e.run("findings", "--file", in, "--no-pr-check", "--no-summary", "--format", "json"); code != 0 {
			t.Fatalf("%s: exit %d %s", in, code, stderr)
		}
	}
	n := 0
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "list issues") && !strings.Contains(c, "--page-size 1 ") {
			n++
			if !strings.Contains(c, "--file internal/api ") && !strings.HasSuffix(c, "--file internal/api") {
				t.Errorf("not normalised: %s", c)
			}
		}
	}
	if n == 0 {
		t.Fatal("no list issues call")
	}
	code, out, _ := e.run("findings", "--file", "./internal/api/**", "--no-pr-check", "--no-summary", "--format", "json")
	if code != 0 || !strings.Contains(out, `"AX-VULN"`) {
		t.Errorf("glob with ./ prefix matched nothing: exit %d\n%s", code, out)
	}
}

// When the transition succeeded but the comment did not, the error and the
// audit log say the state already changed, so nobody retries the transition.
func TestMarkReportsAppliedTransitionWhenCommentFails(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_POST_FAIL", "/api/issues/add_comment")
	code, _, stderr := e.run("mark", "AX-VULN", "fp", "--approved-by-user", "--comment", "fixture only")
	if code != ExitServer || !strings.Contains(stderr, "transition falsepositive was applied") {
		t.Errorf("exit %d: %s", code, stderr)
	}
	a := readAudit(t, e.home)
	if len(a) != 1 || a[0].Result != "error" || !strings.Contains(a[0].ErrorMsg, "was applied") {
		t.Errorf("audit: %+v", a)
	}
}
