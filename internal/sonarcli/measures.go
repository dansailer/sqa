package sonarcli

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Coverage metric keys.
const (
	MetricCoverage               = "coverage"
	MetricLinesToCover           = "lines_to_cover"
	MetricUncoveredLines         = "uncovered_lines"
	MetricUncoveredConditions    = "uncovered_conditions"
	MetricNewCoverage            = "new_coverage"
	MetricNewLinesToCover        = "new_lines_to_cover"
	MetricNewUncoveredLines      = "new_uncovered_lines"
	MetricNewUncoveredConditions = "new_uncovered_conditions"
)

// CoverageMetrics are the overall and new-code coverage metrics sqa reads.
var CoverageMetrics = []string{
	MetricNewCoverage, MetricNewLinesToCover, MetricNewUncoveredLines, MetricNewUncoveredConditions,
	MetricCoverage, MetricLinesToCover, MetricUncoveredLines, MetricUncoveredConditions,
}

// measure is one entry of a measures response. New-code metrics carry their
// value in period (SonarQube 10+/2025.x) or periods[0] (older servers).
type measure struct {
	Metric string `json:"metric"`
	Value  string `json:"value"`
	Period *struct {
		Value string `json:"value"`
	} `json:"period"`
	Periods []struct {
		Value string `json:"value"`
	} `json:"periods"`
}

func (m measure) value() (string, bool) {
	switch {
	case m.Value != "":
		return m.Value, true
	case m.Period != nil && m.Period.Value != "":
		return m.Period.Value, true
	case len(m.Periods) > 0 && m.Periods[0].Value != "":
		return m.Periods[0].Value, true
	}
	return "", false
}

// MeasuredComponent is a project, directory or file with its measures.
type MeasuredComponent struct {
	Key       string
	Path      string
	Qualifier string
	Measures  map[string]float64
}

// Has reports whether metric has a value.
func (c MeasuredComponent) Has(metric string) bool {
	_, ok := c.Measures[metric]
	return ok
}

type rawComponent struct {
	Key       string    `json:"key"`
	Path      string    `json:"path"`
	Qualifier string    `json:"qualifier"`
	Measures  []measure `json:"measures"`
}

func (rc rawComponent) parse() MeasuredComponent {
	c := MeasuredComponent{Key: rc.Key, Path: rc.Path, Qualifier: rc.Qualifier, Measures: map[string]float64{}}
	for _, m := range rc.Measures {
		if v, ok := m.value(); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				c.Measures[m.Metric] = f
			}
		}
	}
	return c
}

// ComponentMeasures calls /api/measures/component.
func (r *Runner) ComponentMeasures(ctx context.Context, component string, metrics []string, branch, pr string) (MeasuredComponent, error) {
	q := url.Values{}
	q.Set("component", component)
	q.Set("metricKeys", strings.Join(metrics, ","))
	setIf(q, "branch", branch)
	setIf(q, "pullRequest", pr)
	var resp struct {
		Component rawComponent `json:"component"`
	}
	if err := r.getJSON(ctx, "/api/measures/component", q, &resp); err != nil {
		return MeasuredComponent{}, err
	}
	return resp.Component.parse(), nil
}

// ComponentTreeOptions maps to /api/measures/component_tree parameters for a
// worst-files-first listing.
type ComponentTreeOptions struct {
	Component   string
	Branch      string
	PullRequest string
	Metrics     []string
	SortMetric  string // e.g. new_uncovered_lines
	NewCode     bool   // sort on the new-code period value
	PageSize    int
}

// ComponentTree calls /api/measures/component_tree for the files below
// Component, sorted by SortMetric descending, and returns them with the total
// number of files that have that measure.
func (r *Runner) ComponentTree(ctx context.Context, o ComponentTreeOptions) ([]MeasuredComponent, int, error) {
	q := url.Values{}
	q.Set("component", o.Component)
	setIf(q, "branch", o.Branch)
	setIf(q, "pullRequest", o.PullRequest)
	q.Set("metricKeys", strings.Join(o.Metrics, ","))
	q.Set("strategy", "leaves")
	q.Set("qualifiers", "FIL")
	q.Set("metricSort", o.SortMetric)
	q.Set("metricSortFilter", "withMeasuresOnly")
	q.Set("asc", "false")
	if o.NewCode {
		q.Set("s", "metricPeriod")
		q.Set("metricPeriodSort", "1")
	} else {
		q.Set("s", "metric")
	}
	ps := o.PageSize
	if ps <= 0 || ps > 500 {
		ps = 500
	}
	q.Set("ps", strconv.Itoa(ps))
	var resp struct {
		Paging struct {
			Total int `json:"total"`
		} `json:"paging"`
		Components []rawComponent `json:"components"`
	}
	if err := r.getJSON(ctx, "/api/measures/component_tree", q, &resp); err != nil {
		return nil, 0, err
	}
	cs := make([]MeasuredComponent, 0, len(resp.Components))
	for _, rc := range resp.Components {
		cs = append(cs, rc.parse())
	}
	return cs, resp.Paging.Total, nil
}

// GateCondition is one quality gate condition from /api/qualitygates/project_status.
type GateCondition struct {
	Status         string `json:"status"`
	MetricKey      string `json:"metricKey"`
	Comparator     string `json:"comparator"`
	ErrorThreshold string `json:"errorThreshold"`
	ActualValue    string `json:"actualValue"`
}

// GateStatus is the quality gate verdict and its conditions.
type GateStatus struct {
	Status     string          `json:"status"`
	Conditions []GateCondition `json:"conditions"`
}

// ProjectStatus calls /api/qualitygates/project_status.
func (r *Runner) ProjectStatus(ctx context.Context, project, branch, pr string) (GateStatus, error) {
	q := url.Values{}
	q.Set("projectKey", project)
	setIf(q, "branch", branch)
	setIf(q, "pullRequest", pr)
	var resp struct {
		ProjectStatus GateStatus `json:"projectStatus"`
	}
	if err := r.getJSON(ctx, "/api/qualitygates/project_status", q, &resp); err != nil {
		return GateStatus{}, err
	}
	return resp.ProjectStatus, nil
}
