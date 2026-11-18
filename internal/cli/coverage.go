package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// Coverage scopes.
const (
	scopeNewCode = "new_code"
	scopeOverall = "overall"
)

// CoverageFile is one file in the worst-first coverage ranking.
type CoverageFile struct {
	Rank                int     `json:"rank"`
	File                string  `json:"file"`
	Coverage            float64 `json:"coverage"`
	UncoveredLines      int     `json:"uncovered_lines"`
	UncoveredConditions int     `json:"uncovered_conditions"`
	LinesToCover        int     `json:"lines_to_cover"`
}

// LineRange is a run of consecutive uncovered lines.
type LineRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// PartialLine is a line whose branches (conditions) are only partly covered.
type PartialLine struct {
	Line       int `json:"line"`
	Covered    int `json:"covered"`
	Conditions int `json:"conditions"`
}

// FileCoverage is the line-level detail for `sqa coverage --file <file>`.
type FileCoverage struct {
	File      string        `json:"file"`
	Uncovered []LineRange   `json:"uncovered"`
	Partial   []PartialLine `json:"partial"`
	Note      string        `json:"note,omitempty"`
}

// GateCheck is a quality gate condition sqa reports.
type GateCheck struct {
	Metric     string `json:"metric"`
	Actual     string `json:"actual"`
	Comparator string `json:"comparator"`
	Threshold  string `json:"threshold"`
	Status     string `json:"status"`
}

// requirement renders the condition as what passes, e.g. ">= 80%".
func (g GateCheck) requirement() string {
	op := map[string]string{"LT": ">=", "GT": "<="}[g.Comparator]
	if op == "" {
		op = g.Comparator
	}
	return op + " " + g.Threshold + "%"
}

func (g GateCheck) verdict() string {
	if g.Status == "OK" {
		return "passing"
	}
	return "failing"
}

// CoverageResult is the output of `sqa coverage`.
type CoverageResult struct {
	Project             string         `json:"project"`
	Branch              string         `json:"branch,omitempty"`
	PullRequest         string         `json:"pull_request,omitempty"`
	Scope               string         `json:"scope"`
	Component           string         `json:"component,omitempty"`
	Coverage            *float64       `json:"coverage"`
	LinesToCover        int            `json:"lines_to_cover"`
	UncoveredLines      int            `json:"uncovered_lines"`
	UncoveredConditions int            `json:"uncovered_conditions"`
	Gate                *GateCheck     `json:"gate,omitempty"`
	TotalFiles          int            `json:"total_files"`
	Files               []CoverageFile `json:"files"`
	Detail              *FileCoverage  `json:"detail,omitempty"`
	Note                string         `json:"note,omitempty"`
	Hint                string         `json:"hint"`
}

func pct(p *float64) string {
	if p == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*p, 'f', 1, 64) + "%"
}

func scopeLabel(scope string) string {
	if scope == scopeNewCode {
		return "new code"
	}
	return "overall code"
}

func (r CoverageResult) header() string {
	var b strings.Builder
	where := r.Project + scopeSuffix(r.Branch, r.PullRequest)
	if r.Component != "" {
		where += " / " + r.Component
	}
	fmt.Fprintf(&b, "Coverage on %s: %s (%d of %d lines uncovered, %d uncovered conditions)", scopeLabel(r.Scope), pct(r.Coverage),
		r.UncoveredLines, r.LinesToCover, r.UncoveredConditions)
	if r.Gate != nil {
		fmt.Fprintf(&b, "; gate %s %s (%s)", r.Gate.Metric, r.Gate.requirement(), r.Gate.verdict())
	}
	return where + "\n" + b.String() + "\n"
}

// Markdown renders the ranking (or file detail) as markdown.
func (r CoverageResult) Markdown() string {
	var b strings.Builder
	b.WriteString("## SonarQube coverage: " + r.header() + "\n")
	if r.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n\n", r.Note)
	}
	if r.Detail != nil {
		b.WriteString(r.Detail.markdown())
		return b.String()
	}
	if len(r.Files) == 0 {
		b.WriteString("No files with uncovered lines.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Showing %d of %d files, most uncovered lines first.\n\n", len(r.Files), r.TotalFiles)
	b.WriteString("| # | file | coverage | uncovered lines | uncovered conditions | lines to cover |\n|---|---|---|---|---|---|\n")
	for _, f := range r.Files {
		fmt.Fprintf(&b, "| %d | %s | %.1f%% | %d | %d | %d |\n", f.Rank, output.MDEscape(f.File), f.Coverage, f.UncoveredLines, f.UncoveredConditions, f.LinesToCover)
	}
	return b.String()
}

// Table renders the ranking (or file detail) for a terminal.
func (r CoverageResult) Table() string {
	var b strings.Builder
	b.WriteString(r.header())
	if r.Note != "" {
		fmt.Fprintf(&b, "note: %s\n", r.Note)
	}
	if r.Detail != nil {
		b.WriteString(r.Detail.markdown())
		return b.String()
	}
	if len(r.Files) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "%-4s %-8s %-9s %-10s %-6s %s\n", "#", "COVERAGE", "UNCOVERED", "CONDITIONS", "LINES", "FILE")
	for _, f := range r.Files {
		fmt.Fprintf(&b, "%-4d %-8s %-9d %-10d %-6d %s\n", f.Rank, strconv.FormatFloat(f.Coverage, 'f', 1, 64)+"%", f.UncoveredLines, f.UncoveredConditions, f.LinesToCover, f.File)
	}
	return b.String()
}

func (d FileCoverage) markdown() string {
	var b strings.Builder
	if d.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", d.Note)
	}
	if len(d.Uncovered) == 0 && len(d.Partial) == 0 {
		if d.Note == "" {
			b.WriteString("No uncovered lines.\n")
		}
		return b.String()
	}
	var rs []string
	for _, r := range d.Uncovered {
		if r.From == r.To {
			rs = append(rs, strconv.Itoa(r.From))
		} else {
			rs = append(rs, fmt.Sprintf("%d-%d", r.From, r.To))
		}
	}
	if len(rs) > 0 {
		fmt.Fprintf(&b, "Uncovered lines: %s\n", strings.Join(rs, ", "))
	}
	var ps []string
	for _, p := range d.Partial {
		ps = append(ps, fmt.Sprintf("%d (%d/%d)", p.Line, p.Covered, p.Conditions))
	}
	if len(ps) > 0 {
		fmt.Fprintf(&b, "Partly covered conditions (line covered/total): %s\n", strings.Join(ps, ", "))
	}
	return b.String()
}

// fileDetail turns /api/sources/lines into uncovered ranges and partly
// covered conditions; onlyNew limits it to lines on new code.
func fileDetail(file string, lines []sonarcli.SourceLine, onlyNew bool) FileCoverage {
	d := FileCoverage{File: file, Uncovered: []LineRange{}, Partial: []PartialLine{}}
	withData := false
	for _, l := range lines {
		if l.LineHits != nil || l.Conditions != nil {
			withData = true
		}
		if onlyNew && !l.IsNew {
			continue
		}
		if l.LineHits != nil && *l.LineHits == 0 {
			if n := len(d.Uncovered); n > 0 && d.Uncovered[n-1].To == l.Line-1 {
				d.Uncovered[n-1].To = l.Line
			} else {
				d.Uncovered = append(d.Uncovered, LineRange{From: l.Line, To: l.Line})
			}
		}
		if l.Conditions != nil && *l.Conditions > 0 {
			covered := 0
			if l.CoveredConditions != nil {
				covered = *l.CoveredConditions
			}
			if covered < *l.Conditions {
				d.Partial = append(d.Partial, PartialLine{Line: l.Line, Covered: covered, Conditions: *l.Conditions})
			}
		}
	}
	if !withData {
		d.Note = "SonarQube returned no line coverage (lineHits) for this file: no coverage report covered it in the last analysis, or the file has no executable lines"
	}
	return d
}

func measureInt(c sonarcli.MeasuredComponent, metric string) int {
	return int(math.Round(c.Measures[metric]))
}

// metricsFor returns (coverage, lines to cover, uncovered lines, uncovered
// conditions) metric keys for a scope.
func metricsFor(scope string) (cov, toCover, unLines, unConds string) {
	if scope == scopeNewCode {
		return sonarcli.MetricNewCoverage, sonarcli.MetricNewLinesToCover, sonarcli.MetricNewUncoveredLines, sonarcli.MetricNewUncoveredConditions
	}
	return sonarcli.MetricCoverage, sonarcli.MetricLinesToCover, sonarcli.MetricUncoveredLines, sonarcli.MetricUncoveredConditions
}

// findGate returns the gate condition on any of metrics, first match wins.
func findGate(st sonarcli.GateStatus, metrics ...string) *GateCheck {
	for _, m := range metrics {
		for _, c := range st.Conditions {
			if c.MetricKey == m {
				return &GateCheck{Metric: c.MetricKey, Actual: c.ActualValue, Comparator: c.Comparator, Threshold: c.ErrorThreshold, Status: c.Status}
			}
		}
	}
	return nil
}

func (a *app) coverageCmd() *cobra.Command {
	var project, branch, pr, file, failOn string
	var top int
	var overall bool
	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Files ranked by uncovered lines on new code, or uncovered lines of one file",
		Long: `Ranks files by uncovered lines on new code (overall code when the project
has no new-code period, or with --overall), with coverage %, uncovered lines
and uncovered conditions, plus the quality gate's coverage condition.

--file <file> shows that file's uncovered line ranges and partly covered
conditions; --file <directory> ranks only the files below it.
--fail-on <percent> exits 1 when coverage in the reported scope is below it.

Read-only. Data comes from the last analysis that imported a coverage report.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var failPct float64
			if failOn != "" {
				v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(failOn), "%"), 64)
				if err != nil || v < 0 || v > 100 {
					return usageErr("--fail-on must be a percentage between 0 and 100")
				}
				failPct = v
			}
			if top < 0 || top > 500 {
				return usageErr("--top must be between 0 and 500")
			}
			r, err := a.runner()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			sc, err := a.resolveScope(ctx, r, project, branch, pr)
			if err != nil {
				return err
			}
			res, err := gatherCoverage(ctx, r, sc, projectPath(file), top, overall)
			if err != nil {
				return err
			}
			if err := a.out(res); err != nil {
				return err
			}
			if failOn != "" && res.Coverage != nil && *res.Coverage < failPct {
				return &ExitError{Code: ExitFindings, Err: fmt.Errorf("coverage on %s %s is below %s%%", scopeLabel(res.Scope), pct(res.Coverage), strconv.FormatFloat(failPct, 'f', -1, 64))}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&project, "project", "p", "", "project key (default: default_project from config, else sonar auto-detection)")
	f.StringVar(&branch, "branch", "", "branch name")
	f.StringVar(&pr, "pr", "", "pull request id")
	f.StringVar(&file, "file", "", "a file (show its uncovered lines) or directory (rank its files), path from project root")
	f.IntVar(&top, "top", 20, "maximum files to show (0 = up to 500)")
	f.BoolVar(&overall, "overall", false, "rank by overall coverage instead of new code")
	f.StringVar(&failOn, "fail-on", "", "exit 1 if coverage in the reported scope is below this percentage")
	return cmd
}

func gatherCoverage(ctx context.Context, r *sonarcli.Runner, sc scope, file string, top int, overall bool) (CoverageResult, error) {
	res := CoverageResult{Project: sc.project, Branch: sc.branch, PullRequest: sc.pr, Files: []CoverageFile{}}
	component := sc.project
	if file != "" {
		component = sc.project + ":" + file
		res.Component = file
	}
	m, err := r.ComponentMeasures(ctx, component, sonarcli.CoverageMetrics, sc.branch, sc.pr)
	if err != nil {
		if file != "" && isNotFound(err) {
			return res, usageErr("no file or directory %q in project %s (use a path from the project root)", file, sc.project)
		}
		return res, err
	}
	res.Scope = scopeNewCode
	switch {
	case overall:
		res.Scope = scopeOverall
	case !m.Has(sonarcli.MetricNewLinesToCover):
		res.Scope = scopeOverall
		res.Note = "no new-code coverage measures (no new-code period or nothing analysed as new code yet); showing overall coverage"
	}
	covK, toCoverK, unLinesK, unCondsK := metricsFor(res.Scope)
	if v, ok := m.Measures[covK]; ok {
		res.Coverage = &v
	}
	res.LinesToCover, res.UncoveredLines, res.UncoveredConditions = measureInt(m, toCoverK), measureInt(m, unLinesK), measureInt(m, unCondsK)
	if !m.Has(toCoverK) && res.Note == "" {
		res.Note = "no coverage measures: the analysis did not import a coverage report"
	}

	if st, err := r.ProjectStatus(ctx, sc.project, sc.branch, sc.pr); err == nil {
		if res.Scope == scopeNewCode {
			res.Gate = findGate(st, sonarcli.MetricNewCoverage, sonarcli.MetricCoverage)
		} else {
			res.Gate = findGate(st, sonarcli.MetricCoverage, sonarcli.MetricNewCoverage)
		}
	}

	if m.Qualifier == "FIL" || m.Qualifier == "UTS" {
		lines, err := r.SourceLines(ctx, component, 0, 0, sc.branch, sc.pr)
		if err != nil {
			return res, err
		}
		d := fileDetail(file, lines, res.Scope == scopeNewCode)
		res.Detail = &d
		res.Hint = "Write tests for the uncovered lines as normal code work: ask the user before branching or opening a PR."
		return res, nil
	}

	ps := top
	if ps == 0 {
		ps = 500
	}
	files, total, err := r.ComponentTree(ctx, sonarcli.ComponentTreeOptions{Component: component, Branch: sc.branch, PullRequest: sc.pr,
		Metrics: sonarcli.CoverageMetrics, SortMetric: unLinesK, NewCode: res.Scope == scopeNewCode, PageSize: ps})
	if err != nil {
		return res, err
	}
	res.TotalFiles = total
	for _, c := range files {
		cf := CoverageFile{File: c.Path, Coverage: c.Measures[covK], UncoveredLines: measureInt(c, unLinesK),
			UncoveredConditions: measureInt(c, unCondsK), LinesToCover: measureInt(c, toCoverK)}
		if cf.File == "" {
			cf.File = strings.TrimPrefix(c.Key, sc.project+":")
		}
		if cf.UncoveredLines == 0 && cf.UncoveredConditions == 0 {
			continue
		}
		cf.Rank = len(res.Files) + 1
		res.Files = append(res.Files, cf)
	}
	res.Hint = "Run `sqa coverage --file <file>` for uncovered lines. Writing tests is normal code work: ask the user before branching or opening a PR."
	return res, nil
}

// isNotFound reports a 404 from the server via `sonar api`.
func isNotFound(err error) bool {
	var se *sonarcli.Error
	return errors.As(err, &se) && se.HTTPStatus() == 404
}

// isNoProject reports sonar's "could not determine project key" failure.
func isNoProject(err error) bool {
	var se *sonarcli.Error
	return errors.As(err, &se) && strings.Contains(se.Stderr, "Could not determine project key")
}
