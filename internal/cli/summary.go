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
	if s.Coverage != nil {
		out = append(out, s.Coverage.line()+". Run `sqa coverage` for the files that need tests.")
	}
	if s.HotspotsReviewed != nil {
		l := s.HotspotsReviewed.line() + "."
		if s.HotspotsReviewed.Status != "" && s.HotspotsReviewed.Status != "OK" {
			l += " Review the hotspots above with the user."
		}
		out = append(out, l)
	}
	if s.Note != "" {
		out = append(out, "Note: "+s.Note)
	}
	return out
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
	return s
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
