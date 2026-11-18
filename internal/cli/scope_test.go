package cli

import (
	"strings"
	"testing"
)

// In CI sonar auto-detects the pull request; the brief `next` prints must
// read the issue, its code and its link from that PR, not the main branch.
func TestNextBriefUsesAutoDetectedPR(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_DETECT_PR", "42")
	n := runNext(t, e, "--no-pr-check")
	if n.Finding == nil || n.Brief == nil {
		t.Fatalf("no brief: %+v", n)
	}
	if !strings.Contains(n.Brief.URL, "pullRequest=42") {
		t.Errorf("brief URL not scoped to the PR: %s", n.Brief.URL)
	}
	var issue, lines bool
	for _, c := range e.calls() {
		if strings.Contains(c, "/api/issues/search?") && strings.Contains(c, "issues="+n.Finding.Key) {
			issue = strings.Contains(c, "pullRequest=42")
		}
		if strings.Contains(c, "/api/sources/lines?") {
			lines = strings.Contains(c, "pullRequest=42")
		}
	}
	if !issue || !lines {
		t.Errorf("issue lookup scoped=%v, code context scoped=%v; calls: %v", issue, lines, e.calls())
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
