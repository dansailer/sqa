package forge

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

func fakeRunner(gh, git string, ghErr, gitErr error, calls *[]call) Runner {
	return func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, call{name, args})
		switch name {
		case "gh":
			return []byte(gh), ghErr
		case "git":
			return []byte(git), gitErr
		}
		return nil, errors.New("unexpected " + name)
	}
}

const prsJSON = `[
 {"number":12,"url":"https://github.com/acme/demo/pull/12","headRefName":"feature/x","body":"[msg](https://sq.example/project/issues?id=demo&issues=AZx_Key-1&open=AZx_Key-1)"},
 {"number":15,"url":"https://github.com/acme/demo/pull/15","headRefName":"sonar/go-s1192-azhot_2","body":""},
 {"number":16,"url":"https://github.com/acme/demo/pull/16","headRefName":"other","body":"mentions AZx_Key-10 only"}
]`

func TestGHMatchesBodyAndBranchOnTokenBoundaries(t *testing.T) {
	var calls []call
	ix := detect(context.Background(), "", fakeRunner(prsJSON, "", nil, nil, &calls))
	if ix.Source != SourceGH || ix.Note != "" {
		t.Fatalf("index: %+v", ix)
	}
	if len(calls) != 1 || calls[0].name != "gh" || !strings.Contains(strings.Join(calls[0].args, " "), "pr list --state open") {
		t.Fatalf("want exactly one gh pr list call, got %+v", calls)
	}
	if p, ok := ix.Lookup("AZx_Key-1"); !ok || p.Number != 12 || p.Ref() != "https://github.com/acme/demo/pull/12" {
		t.Errorf("body match: %+v %v", p, ok)
	}
	if p, ok := ix.Lookup("AZhot_2"); !ok || p.Number != 15 {
		t.Errorf("branch match (case and separator insensitive): %+v %v", p, ok)
	}
	// AZx_Key-10 appears only in PR 16; AZx_Key prefix keys must not cross-match.
	if p, ok := ix.Lookup("AZx_Key-10"); !ok || p.Number != 16 {
		t.Errorf("exact token: %+v %v", p, ok)
	}
	for _, k := range []string{"AZx_Key", "Key-1", "hot_2x", ""} {
		if p, ok := ix.Lookup(k); ok {
			t.Errorf("%q must not match, got %+v", k, p)
		}
	}
}

func TestFallsBackToGitBranches(t *testing.T) {
	var calls []call
	heads := "abc\trefs/heads/sonar/go-s3649-ax-vuln\ndef\trefs/heads/sonar/go-s1-ax-vuln-old\n"
	ix := detect(context.Background(), "", fakeRunner("", heads, errors.New("gh: not logged in"), nil, &calls))
	if ix.Source != SourceGit || !strings.Contains(ix.Note, "gh: not logged in") || !strings.Contains(ix.Note, "remote sonar/* branches") {
		t.Fatalf("index: %+v", ix)
	}
	if len(calls) != 2 || calls[1].name != "git" || strings.Join(calls[1].args, " ") != "ls-remote --heads origin refs/heads/sonar/*" {
		t.Fatalf("calls: %+v", calls)
	}
	p, ok := ix.Lookup("AX-VULN")
	if !ok || p.Branch != "sonar/go-s3649-ax-vuln" || p.Ref() != "branch:sonar/go-s3649-ax-vuln" || p.URL != "" {
		t.Errorf("lookup: %+v %v", p, ok)
	}
	if p, ok := ix.Lookup("AX-VULN-OLD"); !ok || p.Branch != "sonar/go-s1-ax-vuln-old" {
		t.Errorf("old: %+v %v", p, ok)
	}
}

func TestDegradesWhenNeitherWorks(t *testing.T) {
	var calls []call
	ix := detect(context.Background(), "", fakeRunner("", "", errors.New("gh not found on PATH"), errors.New("git: fatal: not a git repository"), &calls))
	if ix.Available() || !strings.Contains(ix.Note, "unavailable") || !strings.Contains(ix.Note, "not a git repository") {
		t.Fatalf("index: %+v", ix)
	}
	if _, ok := ix.Lookup("AX-VULN"); ok {
		t.Error("empty index must not match")
	}
}

func TestBadGHOutputFallsBack(t *testing.T) {
	var calls []call
	ix := detect(context.Background(), "", fakeRunner("not json", "", nil, nil, &calls))
	if ix.Source != SourceGit || !strings.Contains(ix.Note, "unexpected output") {
		t.Fatalf("index: %+v", ix)
	}
}

func TestOneBodyListingSeveralKeysMatchesEach(t *testing.T) {
	var calls []call
	body := "### Resolves SonarQube findings\n\n- [a](https://sq/project/issues?id=demo&issues=AZa-1&open=AZa-1) (key `AZa-1`)\n- [b](https://sq/security_hotspots?id=demo&hotspots=AZb_2) (key `AZb_2`)\n"
	js := `[{"number":7,"url":"https://github.com/acme/demo/pull/7","headRefName":"sonar/exclude-fixtures","body":` + strconv.Quote(body) + `}]`
	ix := detect(context.Background(), "", fakeRunner(js, "", nil, nil, &calls))
	for _, k := range []string{"AZa-1", "AZb_2"} {
		if p, ok := ix.Lookup(k); !ok || p.Number != 7 {
			t.Errorf("%s: %+v %v", k, p, ok)
		}
	}
	if _, ok := ix.Lookup("AZa"); ok {
		t.Error("prefix must not match")
	}
}
