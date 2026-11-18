package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/dansailer/sqa/internal/sonarcli"
)

// Hotspot review metrics (percentage of hotspots reviewed).
const (
	metricHotspotsReviewed    = "security_hotspots_reviewed"
	metricNewHotspotsReviewed = "new_security_hotspots_reviewed"
)

// GateSummary is the short quality gate read-out at the end of `sqa findings`:
// coverage and hotspot review against the gate's conditions.
type GateSummary struct {
	Status           string     `json:"status,omitempty"`
	Coverage         *GateCheck `json:"coverage,omitempty"`
	HotspotsReviewed *GateCheck `json:"hotspots_reviewed,omitempty"`
	Note             string     `json:"note,omitempty"`
	// NextSteps says what to tell the user when the gate is failing.
	NextSteps []string `json:"next_steps,omitempty"`
}

func failing(c *GateCheck) bool {
	return c != nil && c.Comparator != "" && c.Status != "" && c.Status != "OK"
}

// nextSteps lists what an agent must tell the user about a failing gate.
func (s GateSummary) nextSteps() []string {
	var out []string
	if failing(s.Coverage) {
		out = append(out, "Coverage is below the quality gate: tell the user and offer `sqa coverage` to find the files that need tests.")
	}
	if failing(s.HotspotsReviewed) {
		out = append(out, "Security hotspots reviewed is below the quality gate: tell the user; the hotspots in the queue need a review the user approves.")
	}
	if len(out) == 0 && s.Status == "ERROR" {
		out = append(out, "The quality gate is failing on other conditions: tell the user and check the project in SonarQube.")
	}
	return out
}

func metricLabel(metric string) string {
	switch metric {
	case sonarcli.MetricNewCoverage:
		return "Coverage on new code"
	case sonarcli.MetricCoverage:
		return "Coverage"
	case metricNewHotspotsReviewed:
		return "Security hotspots reviewed on new code"
	case metricHotspotsReviewed:
		return "Security hotspots reviewed"
	}
	return metric
}

func (g GateCheck) line() string {
	v := g.Actual
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		v = strconv.FormatFloat(f, 'f', 1, 64) + "%"
	} else if v == "" {
		v = "n/a"
	}
	s := metricLabel(g.Metric) + ": " + v
	if g.Comparator != "" {
		s += " (gate requires " + g.requirement() + ": " + g.verdict() + ")"
	} else {
		s += " (no condition in the quality gate)"
	}
	return s
}

// Lines renders the summary as text lines.
func (s GateSummary) Lines() []string {
	var out []string
	if s.Status != "" {
		out = append(out, "Quality gate: "+s.Status)
	}
	for _, c := range []*GateCheck{s.Coverage, s.HotspotsReviewed} {
		if c != nil {
			out = append(out, c.line())
		}
	}
	if s.Note != "" {
		out = append(out, "Note: "+s.Note)
	}
	for _, n := range s.NextSteps {
		out = append(out, "Next step: "+n)
	}
	return out
}

// text renders the summary with each line prefixed.
func (s *GateSummary) text(prefix string) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	for _, l := range s.Lines() {
		b.WriteString(prefix + l + "\n")
	}
	return b.String()
}

// gateSummary reads the quality gate once (/api/qualitygates/project_status).
// Only when the gate has no coverage condition does it make one more call
// (/api/measures/component) for the coverage value. It never fails: errors
// end up in Note.
func gateSummary(ctx context.Context, r *sonarcli.Runner, project, branch, pr string) *GateSummary {
	s := &GateSummary{}
	st, err := r.ProjectStatus(ctx, project, branch, pr)
	if err != nil {
		s.Note = "quality gate unavailable: " + firstLine(err.Error())
	} else {
		s.Status = st.Status
		s.Coverage = findGate(st, sonarcli.MetricNewCoverage, sonarcli.MetricCoverage)
		s.HotspotsReviewed = findGate(st, metricNewHotspotsReviewed, metricHotspotsReviewed)
	}
	if s.Coverage == nil {
		m, err := r.ComponentMeasures(ctx, project, []string{sonarcli.MetricNewCoverage, sonarcli.MetricCoverage}, branch, pr)
		switch {
		case err != nil:
			if s.Note == "" {
				s.Note = "coverage unavailable: " + firstLine(err.Error())
			}
		case m.Has(sonarcli.MetricNewCoverage):
			s.Coverage = &GateCheck{Metric: sonarcli.MetricNewCoverage, Actual: fmtFloat(m.Measures[sonarcli.MetricNewCoverage])}
		case m.Has(sonarcli.MetricCoverage):
			s.Coverage = &GateCheck{Metric: sonarcli.MetricCoverage, Actual: fmtFloat(m.Measures[sonarcli.MetricCoverage])}
		}
	}
	s.NextSteps = s.nextSteps()
	return s
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
