package sonarcli

import (
	"html"
	"regexp"
	"strings"
)

// TextRange is a SonarQube text range.
type TextRange struct {
	StartLine   int `json:"startLine"`
	EndLine     int `json:"endLine"`
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
}

// Location is one step in a flow (data-flow / secondary locations).
type Location struct {
	Component string     `json:"component"`
	TextRange *TextRange `json:"textRange,omitempty"`
	Msg       string     `json:"msg,omitempty"`
}

// Flow is a list of locations.
type Flow struct {
	Locations []Location `json:"locations"`
}

// Impact is an MQR software-quality impact.
type Impact struct {
	SoftwareQuality string `json:"softwareQuality"`
	Severity        string `json:"severity"`
}

// Issue is one element of /api/issues/search "issues" (the body that
// `sonar list issues --format json` prints unchanged).
type Issue struct {
	Key                string     `json:"key"`
	Rule               string     `json:"rule"`
	Severity           string     `json:"severity"`
	Component          string     `json:"component"`
	Project            string     `json:"project"`
	Line               int        `json:"line,omitempty"`
	TextRange          *TextRange `json:"textRange,omitempty"`
	Flows              []Flow     `json:"flows,omitempty"`
	Status             string     `json:"status"`
	IssueStatus        string     `json:"issueStatus,omitempty"`
	Resolution         string     `json:"resolution,omitempty"`
	Message            string     `json:"message"`
	Effort             string     `json:"effort,omitempty"`
	Author             string     `json:"author,omitempty"`
	Tags               []string   `json:"tags,omitempty"`
	Type               string     `json:"type"`
	Impacts            []Impact   `json:"impacts,omitempty"`
	CleanCodeAttribute string     `json:"cleanCodeAttribute,omitempty"`
	CreationDate       string     `json:"creationDate,omitempty"`
	UpdateDate         string     `json:"updateDate,omitempty"`
	Transitions        []string   `json:"transitions,omitempty"`
	QuickFixAvailable  bool       `json:"quickFixAvailable,omitempty"`
	RuleDescriptionCtx string     `json:"ruleDescriptionContextKey,omitempty"`
	Branch             string     `json:"branch,omitempty"`
	PullRequest        string     `json:"pullRequest,omitempty"`
}

// Paging is SonarQube's paging block.
type Paging struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
	Total     int `json:"total"`
}

// Component describes a file or project.
type Component struct {
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	LongName  string `json:"longName,omitempty"`
	Qualifier string `json:"qualifier,omitempty"`
	Path      string `json:"path,omitempty"`
}

// IssuesSearchResponse is the /api/issues/search body.
type IssuesSearchResponse struct {
	Total      int         `json:"total"`
	Paging     Paging      `json:"paging"`
	Issues     []Issue     `json:"issues"`
	Components []Component `json:"components,omitempty"`
}

// Hotspot is one element of /api/hotspots/search "hotspots".
type Hotspot struct {
	Key                      string     `json:"key"`
	Component                string     `json:"component"`
	Project                  string     `json:"project"`
	SecurityCategory         string     `json:"securityCategory"`
	VulnerabilityProbability string     `json:"vulnerabilityProbability"`
	Status                   string     `json:"status"`
	Resolution               string     `json:"resolution,omitempty"`
	Line                     int        `json:"line,omitempty"`
	Message                  string     `json:"message"`
	Author                   string     `json:"author,omitempty"`
	CreationDate             string     `json:"creationDate,omitempty"`
	UpdateDate               string     `json:"updateDate,omitempty"`
	TextRange                *TextRange `json:"textRange,omitempty"`
	RuleKey                  string     `json:"ruleKey"`
	Flows                    []Flow     `json:"flows,omitempty"`
}

// HotspotsSearchResponse is the /api/hotspots/search body.
type HotspotsSearchResponse struct {
	Paging     Paging      `json:"paging"`
	Hotspots   []Hotspot   `json:"hotspots"`
	Components []Component `json:"components,omitempty"`
}

// HotspotRule is the rule block inside /api/hotspots/show.
type HotspotRule struct {
	Key                      string `json:"key"`
	Name                     string `json:"name"`
	SecurityCategory         string `json:"securityCategory"`
	VulnerabilityProbability string `json:"vulnerabilityProbability"`
	RiskDescription          string `json:"riskDescription,omitempty"`
	VulnerabilityDescription string `json:"vulnerabilityDescription,omitempty"`
	FixRecommendations       string `json:"fixRecommendations,omitempty"`
}

// HotspotDetail is the /api/hotspots/show body.
type HotspotDetail struct {
	Key        string      `json:"key"`
	Component  Component   `json:"component"`
	Project    Component   `json:"project"`
	Rule       HotspotRule `json:"rule"`
	Status     string      `json:"status"`
	Resolution string      `json:"resolution,omitempty"`
	Line       int         `json:"line,omitempty"`
	Message    string      `json:"message"`
	Author     string      `json:"author,omitempty"`
	TextRange  *TextRange  `json:"textRange,omitempty"`
	Flows      []Flow      `json:"flows,omitempty"`
}

// AsHotspot returns the search-result view of the hotspot.
func (h HotspotDetail) AsHotspot() Hotspot {
	return Hotspot{Key: h.Key, Component: h.Component.Key, Project: h.Project.Key, VulnerabilityProbability: h.Rule.VulnerabilityProbability,
		Status: h.Status, Resolution: h.Resolution, Line: h.Line, Message: h.Message, Author: h.Author, TextRange: h.TextRange,
		RuleKey: h.Rule.Key, Flows: h.Flows}
}

// DescriptionSection is one section of a rule description.
type DescriptionSection struct {
	Key     string `json:"key"`
	Content string `json:"content"`
	Context *struct {
		Key         string `json:"key"`
		DisplayName string `json:"displayName"`
	} `json:"context,omitempty"`
}

// Rule is the "rule" object of /api/rules/show.
type Rule struct {
	Key                 string               `json:"key"`
	Name                string               `json:"name"`
	Lang                string               `json:"lang,omitempty"`
	LangName            string               `json:"langName,omitempty"`
	Severity            string               `json:"severity,omitempty"`
	Type                string               `json:"type,omitempty"`
	HTMLDesc            string               `json:"htmlDesc,omitempty"`
	MDDesc              string               `json:"mdDesc,omitempty"`
	DescriptionSections []DescriptionSection `json:"descriptionSections,omitempty"`
	Impacts             []Impact             `json:"impacts,omitempty"`
	CleanCodeAttribute  string               `json:"cleanCodeAttribute,omitempty"`
	SysTags             []string             `json:"sysTags,omitempty"`
}

// SourceLine is one line of /api/sources/lines. Code is HTML-highlighted.
type SourceLine struct {
	Line  int    `json:"line"`
	Code  string `json:"code"`
	IsNew bool   `json:"isNew,omitempty"`
	// Coverage fields are present only on lines the imported coverage report
	// covers (nil otherwise).
	LineHits          *int `json:"lineHits,omitempty"`
	Conditions        *int `json:"conditions,omitempty"`
	CoveredConditions *int `json:"coveredConditions,omitempty"`
}

var reTags = regexp.MustCompile(`<[^>]*>`)

// PlainText strips HTML markup and unescapes entities.
func PlainText(s string) string {
	return html.UnescapeString(reTags.ReplaceAllString(s, ""))
}

// HTMLToText converts a rule description section (HTML) into compact text,
// keeping paragraph and code-block breaks.
func HTMLToText(s string) string {
	r := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</h2>", "\n", "</h3>", "\n", "</h4>", "\n",
		"</li>", "\n", "<li>", "- ", "<pre>", "\n```\n", "</pre>", "\n```\n",
	)
	t := PlainText(r.Replace(s))
	lines := strings.Split(t, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// FilePath returns the project-relative path of a component key
// ("project:src/a.go" -> "src/a.go"), preferring the components list.
func FilePath(componentKey string, comps []Component) string {
	for _, c := range comps {
		if c.Key == componentKey && c.Path != "" {
			return c.Path
		}
	}
	if i := strings.Index(componentKey, ":"); i >= 0 {
		return componentKey[i+1:]
	}
	return componentKey
}
