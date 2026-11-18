// Package findings normalises SonarQube issues and security hotspots into one
// model and ranks them security-first.
package findings

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dansailer/sqa/internal/sonarcli"
)

// Kind distinguishes issues from security hotspots.
type Kind string

// Finding kinds.
const (
	KindIssue   Kind = "issue"
	KindHotspot Kind = "hotspot"
)

// Tier is the security-first priority bucket (1 = most urgent).
type Tier int

// Tiers, in priority order.
const (
	TierVulnerability   Tier = 1
	TierHotspot         Tier = 2
	TierReliability     Tier = 3
	TierMaintainability Tier = 4
)

// Label returns a short tier name.
func (t Tier) Label() string {
	switch t {
	case TierVulnerability:
		return "vulnerability"
	case TierHotspot:
		return "hotspot"
	case TierReliability:
		return "reliability"
	case TierMaintainability:
		return "maintainability"
	}
	return "unknown"
}

// Severity is the unified severity scale (MQR names).
type Severity int

// Unified severities, low to high.
const (
	SevUnknown Severity = iota
	SevInfo
	SevLow
	SevMedium
	SevHigh
	SevBlocker
)

func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "INFO"
	case SevLow:
		return "LOW"
	case SevMedium:
		return "MEDIUM"
	case SevHigh:
		return "HIGH"
	case SevBlocker:
		return "BLOCKER"
	}
	return "UNKNOWN"
}

// ParseSeverity accepts MQR (BLOCKER, HIGH, MEDIUM, LOW, INFO) and standard
// (BLOCKER, CRITICAL, MAJOR, MINOR, INFO) severities and maps them to one
// scale: CRITICAL=HIGH, MAJOR=MEDIUM, MINOR=LOW.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BLOCKER":
		return SevBlocker, nil
	case "HIGH", "CRITICAL":
		return SevHigh, nil
	case "MEDIUM", "MAJOR":
		return SevMedium, nil
	case "LOW", "MINOR":
		return SevLow, nil
	case "INFO":
		return SevInfo, nil
	}
	return SevUnknown, fmt.Errorf("unknown severity %q (use BLOCKER, HIGH|CRITICAL, MEDIUM|MAJOR, LOW|MINOR, INFO)", s)
}

func sev(s string) Severity {
	v, _ := ParseSeverity(s)
	return v
}

// Finding is the normalised view of an issue or hotspot.
type Finding struct {
	Rank        int      `json:"rank"`
	Key         string   `json:"key"`
	Kind        Kind     `json:"kind"`
	Tier        Tier     `json:"tier"`
	Category    string   `json:"category"`
	Quality     string   `json:"quality"`
	Severity    Severity `json:"-"`
	SeverityStr string   `json:"severity"`
	Rule        string   `json:"rule"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	NewCode     bool     `json:"new_code"`
	Status      string   `json:"status"`
	Message     string   `json:"message"`
	// PR is the open fix PR (URL) or `branch:<name>` (remote fix branch,
	// git fallback); empty when none was found or the check was off.
	PR string `json:"pr"`
}

// qualityTier orders MQR software qualities.
var qualityTier = map[string]Tier{
	"SECURITY":        TierVulnerability,
	"RELIABILITY":     TierReliability,
	"MAINTAINABILITY": TierMaintainability,
}

var typeTier = map[string]struct {
	tier    Tier
	quality string
}{
	"VULNERABILITY": {TierVulnerability, "SECURITY"},
	"BUG":           {TierReliability, "RELIABILITY"},
	"CODE_SMELL":    {TierMaintainability, "MAINTAINABILITY"},
}

// FromIssue normalises an issue. With MQR impacts, the most security-relevant
// impact decides the tier and its severity is used; otherwise the standard
// type/severity are mapped.
func FromIssue(i sonarcli.Issue, comps []sonarcli.Component, newCode bool) Finding {
	f := Finding{
		Key: i.Key, Kind: KindIssue, Rule: i.Rule, File: sonarcli.FilePath(i.Component, comps),
		Line: i.Line, NewCode: newCode, Message: i.Message, Status: i.IssueStatus,
		Tier: TierMaintainability, Quality: "MAINTAINABILITY",
	}
	if f.Status == "" {
		f.Status = i.Status
	}
	if f.Line == 0 && i.TextRange != nil {
		f.Line = i.TextRange.StartLine
	}
	if len(i.Impacts) > 0 {
		best := Tier(99)
		for _, im := range i.Impacts {
			t, ok := qualityTier[strings.ToUpper(im.SoftwareQuality)]
			if !ok {
				continue
			}
			s := sev(im.Severity)
			if t < best || (t == best && s > f.Severity) {
				best, f.Tier, f.Quality, f.Severity = t, t, strings.ToUpper(im.SoftwareQuality), s
			}
		}
	}
	if f.Severity == SevUnknown {
		if tt, ok := typeTier[strings.ToUpper(i.Type)]; ok && len(i.Impacts) == 0 {
			f.Tier, f.Quality = tt.tier, tt.quality
		}
		f.Severity = sev(i.Severity)
	}
	f.Category = f.Tier.Label()
	f.SeverityStr = f.Severity.String()
	return f
}

// FromHotspot normalises a security hotspot; its vulnerability probability
// becomes the severity.
func FromHotspot(h sonarcli.Hotspot, comps []sonarcli.Component, newCode bool) Finding {
	f := Finding{
		Key: h.Key, Kind: KindHotspot, Tier: TierHotspot, Quality: "SECURITY", Rule: h.RuleKey,
		File: sonarcli.FilePath(h.Component, comps), Line: h.Line, NewCode: newCode,
		Status: h.Status, Message: h.Message, Severity: sev(h.VulnerabilityProbability),
	}
	if h.Resolution != "" {
		f.Status += "/" + h.Resolution
	}
	if f.Line == 0 && h.TextRange != nil {
		f.Line = h.TextRange.StartLine
	}
	f.Category = f.Tier.Label()
	f.SeverityStr = f.Severity.String()
	return f
}

// Less implements the security-first order: tier, severity (desc), new code
// first, file path, line, key.
func Less(a, b Finding) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Severity != b.Severity {
		return a.Severity > b.Severity
	}
	if a.NewCode != b.NewCode {
		return a.NewCode
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Key < b.Key
}

// Rank sorts findings in place and assigns 1-based ranks.
func Rank(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return Less(fs[i], fs[j]) })
	for i := range fs {
		fs[i].Rank = i + 1
	}
}

// Filter options.
type Filter struct {
	MinSeverity  Severity
	SecurityOnly bool
}

// Apply returns the findings matching the filter.
func (f Filter) Apply(in []Finding) []Finding {
	out := in[:0:0]
	for _, x := range in {
		if f.SecurityOnly && x.Tier > TierHotspot {
			continue
		}
		if x.Severity < f.MinSeverity {
			continue
		}
		out = append(out, x)
	}
	return out
}

// AnyAtLeast reports whether a finding at or above s exists.
func AnyAtLeast(fs []Finding, s Severity) bool {
	for _, f := range fs {
		if f.Severity >= s {
			return true
		}
	}
	return false
}
