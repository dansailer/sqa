package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRBodyForKeys(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("pr-body", "AX-VULN", "HS-1", "AX-VULN")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	for _, must := range []string{
		"### Resolves SonarQube findings\n\n",
		"- [Change this code to not construct SQL queries directly from user-controlled data.](https://sonarqube.example.com/project/issues?",
		"issues=AX-VULN", "(`go:S3649` Database queries should not be vulnerable to injection attacks · `internal/api/users.go:27` · key `AX-VULN`)",
		"https://sonarqube.example.com/security_hotspots?", "hotspots=HS-1", "key `HS-1`",
	} {
		if !strings.Contains(out, must) {
			t.Errorf("missing %q in:\n%s", must, out)
		}
	}
	if strings.Count(out, "key `AX-VULN`") != 1 {
		t.Errorf("duplicate keys must be listed once:\n%s", out)
	}
	if strings.Contains(out, "Resolved by:") {
		t.Error("no reason given")
	}
	// Markdown by default even when piped (it goes into a PR body).
	if strings.Contains(out, "markdown:") || strings.HasPrefix(out, "{") {
		t.Errorf("default must be raw Markdown:\n%s", out)
	}
	if len(e.posts()) != 0 {
		t.Error("pr-body must be read-only")
	}

	code, out, _ = e.run("pr-body", "AX-VULN", "--format", "json", "--reason", "excluding fixtures via sonar.exclusions")
	var res PRBody
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 {
		t.Fatal(err, code, out)
	}
	if len(res.Findings) != 1 || res.Findings[0].RuleName == "" || !strings.Contains(res.Body, "Resolved by: excluding fixtures via sonar.exclusions") {
		t.Errorf("json: %+v", res)
	}

	for _, bad := range [][]string{{"pr-body"}, {"pr-body", "NOPE"}, {"pr-body", "AX-VULN", "--from-findings"}} {
		if code, _, _ := e.run(bad...); code != ExitUsage {
			t.Errorf("%v: exit %d, want 2", bad, code)
		}
	}
}

func TestPRBodyFromFindingsWithGlob(t *testing.T) {
	e := newEnv(t)
	code, out, stderr := e.run("pr-body", "--from-findings", "--file", "internal/api/**", "--reason", "exclude internal/api from analysis")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if !strings.Contains(out, "key `AX-VULN`") || !strings.Contains(out, "key `HS-1`") || strings.Contains(out, "AX-BUG") || strings.Contains(out, "HS-2") {
		t.Errorf("glob selection:\n%s", out)
	}
	if !strings.Contains(out, "Resolved by: exclude internal/api from analysis") {
		t.Errorf("reason:\n%s", out)
	}
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "list issues") && strings.Contains(c, "--file") {
			t.Errorf("a glob must not be passed to sonar: %s", c)
		}
		if strings.HasPrefix(c, "api get /api/hotspots/search") && strings.Contains(c, "files=") {
			t.Errorf("a glob must not be passed to the hotspot search: %s", c)
		}
	}
	if code, _, _ := e.run("pr-body", "--from-findings", "--file", "nothing/**"); code != ExitUsage {
		t.Errorf("no matches: exit %d", code)
	}
}

func TestFindingsFileGlob(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("findings", "--format", "json", "--file", "**/*.go", "--no-pr-check", "--no-summary")
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 {
		t.Fatal(err, code)
	}
	if len(res.Findings) != 6 {
		t.Errorf("**/*.go should match all 6: %d", len(res.Findings))
	}
	code, out, _ = e.run("findings", "--format", "json", "--file", "internal/*/users.go", "--no-pr-check", "--no-summary")
	res = FindingsResult{}
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 {
		t.Fatal(err, code)
	}
	if len(res.Findings) != 1 || res.Findings[0].Key != "AX-VULN" {
		t.Errorf("single-segment glob: %+v", res.Findings)
	}
	// A plain path still goes to sonar unchanged.
	e.run("findings", "--format", "json", "--file", "internal/api/users.go", "--no-pr-check", "--no-summary")
	var sawFile bool
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "list issues") && strings.Contains(c, "--file internal/api/users.go") {
			sawFile = true
		}
	}
	if !sawFile {
		t.Errorf("plain --file not passed through: %v", e.calls())
	}
}

// A PR whose body is the pr-body block for several findings makes next and
// findings treat every listed finding as having that PR.
func TestGroupedPRBodyCoversEveryListedFinding(t *testing.T) {
	e := newEnv(t)
	code, body, stderr := e.run("pr-body", "AX-VULN", "AX-VULN-OLD")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	prs, err := json.Marshal([]map[string]any{{"number": 41, "url": "https://github.com/acme/demo/pull/41", "headRefName": "sonar/exclude-fixtures", "body": "Exclude fixtures.\n\n" + body}})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "prs.json")
	if err := os.WriteFile(p, prs, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GH_JSON", p)
	n := runNext(t, e)
	if n.Finding == nil || n.Finding.Key != "HS-1" || len(n.Skipped) != 2 {
		t.Fatalf("next=%+v skipped=%+v", n.Finding, n.Skipped)
	}
	for _, sk := range n.Skipped {
		if sk.PR != "https://github.com/acme/demo/pull/41" || sk.Reason != "open PR" {
			t.Errorf("skipped: %+v", sk)
		}
	}
}
