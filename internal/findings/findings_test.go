package findings

import (
	"testing"

	"github.com/dansailer/sqa/internal/sonarcli"
)

func TestParseSeverityMapsStandardToMQR(t *testing.T) {
	cases := map[string]Severity{
		"BLOCKER": SevBlocker, "critical": SevHigh, "HIGH": SevHigh,
		"MAJOR": SevMedium, "MEDIUM": SevMedium, "MINOR": SevLow, "LOW": SevLow, "INFO": SevInfo,
	}
	for in, want := range cases {
		got, err := ParseSeverity(in)
		if err != nil || got != want {
			t.Errorf("%s: got %v %v want %v", in, got, err, want)
		}
	}
	if _, err := ParseSeverity("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestFromIssueMQRPicksSecurityImpact(t *testing.T) {
	i := sonarcli.Issue{
		Key: "I1", Type: "CODE_SMELL", Severity: "MAJOR", Component: "p:a.go",
		Impacts: []sonarcli.Impact{
			{SoftwareQuality: "MAINTAINABILITY", Severity: "BLOCKER"},
			{SoftwareQuality: "SECURITY", Severity: "LOW"},
		},
	}
	f := FromIssue(i, nil, false)
	if f.Tier != TierVulnerability || f.Severity != SevLow || f.Quality != "SECURITY" || f.File != "a.go" {
		t.Fatalf("unexpected %+v", f)
	}
}

func TestFromIssueStandardMode(t *testing.T) {
	f := FromIssue(sonarcli.Issue{Key: "I2", Type: "BUG", Severity: "CRITICAL", Status: "OPEN"}, nil, true)
	if f.Tier != TierReliability || f.Severity != SevHigh || !f.NewCode || f.Status != "OPEN" {
		t.Fatalf("unexpected %+v", f)
	}
}

func TestRankSecurityFirst(t *testing.T) {
	fs := []Finding{
		{Key: "smell-blocker", Tier: TierMaintainability, Severity: SevBlocker},
		{Key: "bug-high", Tier: TierReliability, Severity: SevHigh},
		{Key: "hot-low", Tier: TierHotspot, Severity: SevLow},
		{Key: "hot-high", Tier: TierHotspot, Severity: SevHigh},
		{Key: "vuln-low-old", Tier: TierVulnerability, Severity: SevLow, File: "a"},
		{Key: "vuln-low-new", Tier: TierVulnerability, Severity: SevLow, NewCode: true, File: "z"},
		{Key: "vuln-med-b2", Tier: TierVulnerability, Severity: SevMedium, File: "b", Line: 20},
		{Key: "vuln-med-b1", Tier: TierVulnerability, Severity: SevMedium, File: "b", Line: 3},
	}
	Rank(fs)
	want := []string{"vuln-med-b1", "vuln-med-b2", "vuln-low-new", "vuln-low-old", "hot-high", "hot-low", "bug-high", "smell-blocker"}
	for i, k := range want {
		if fs[i].Key != k || fs[i].Rank != i+1 {
			t.Fatalf("pos %d: got %s (rank %d), want %s", i, fs[i].Key, fs[i].Rank, k)
		}
	}
}

func TestFilter(t *testing.T) {
	fs := []Finding{
		{Key: "a", Tier: TierVulnerability, Severity: SevLow},
		{Key: "b", Tier: TierHotspot, Severity: SevHigh},
		{Key: "c", Tier: TierReliability, Severity: SevBlocker},
	}
	got := Filter{SecurityOnly: true}.Apply(fs)
	if len(got) != 2 {
		t.Fatalf("security-only: %v", got)
	}
	got = Filter{MinSeverity: SevHigh}.Apply(fs)
	if len(got) != 2 || got[0].Key != "b" {
		t.Fatalf("min severity: %v", got)
	}
	if !AnyAtLeast(fs, SevBlocker) || AnyAtLeast(fs[:2], SevBlocker) {
		t.Fatal("AnyAtLeast")
	}
}
