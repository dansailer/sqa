package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/findings"
	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

type scopeFlags struct {
	project, branch, pr, file string
	newCode                   bool
	minSeverity               string
	securityOnly              bool
}

func (s *scopeFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&s.project, "project", "p", "", "project key (default: default_project from config, else sonar auto-detection)")
	f.StringVar(&s.branch, "branch", "", "branch name")
	f.StringVar(&s.pr, "pr", "", "pull request id")
	f.StringVar(&s.file, "file", "", "limit to one file (or a directory's own files), path from project root")
	f.BoolVar(&s.newCode, "new-code", false, "only findings on new code")
	f.StringVar(&s.minSeverity, "min-severity", "", "minimum severity: BLOCKER, HIGH|CRITICAL, MEDIUM|MAJOR, LOW|MINOR, INFO")
	f.BoolVar(&s.securityOnly, "security-only", false, "only vulnerabilities and security hotspots")
}

// FindingsResult is the output of `sqa findings`.
type FindingsResult struct {
	Project     string             `json:"project"`
	Branch      string             `json:"branch,omitempty"`
	PullRequest string             `json:"pull_request,omitempty"`
	Total       int                `json:"total"`
	Shown       int                `json:"shown"`
	Findings    []findings.Finding `json:"findings"`
}

// Markdown renders a markdown table.
func (r FindingsResult) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## SonarQube findings: %s%s\n\n", r.Project, scopeSuffix(r.Branch, r.PullRequest))
	fmt.Fprintf(&b, "Showing %d of %d, ranked security first.\n\n", r.Shown, r.Total)
	if len(r.Findings) == 0 {
		b.WriteString("No open findings.\n")
		return b.String()
	}
	b.WriteString("| # | category | severity | key | rule | location | new | message |\n|---|---|---|---|---|---|---|---|\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | %s |\n", f.Rank, f.Category, f.SeverityStr, f.Key, f.Rule,
			output.MDEscape(loc(f)), yesNo(f.NewCode), output.MDEscape(f.Message))
	}
	return b.String()
}

// Table renders a terminal table.
func (r FindingsResult) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s: %d of %d findings (security first)\n", r.Project, scopeSuffix(r.Branch, r.PullRequest), r.Shown, r.Total)
	if len(r.Findings) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "%-4s %-15s %-8s %-3s %-22s %-40s %s\n", "#", "CATEGORY", "SEVERITY", "NEW", "KEY", "LOCATION", "MESSAGE")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%-4d %-15s %-8s %-3s %-22s %-40s %s\n", f.Rank, f.Category, f.SeverityStr, yesNo(f.NewCode), f.Key, clip(loc(f), 40), clip(f.Message, 90))
	}
	return b.String()
}

func scopeSuffix(branch, pr string) string {
	switch {
	case pr != "":
		return " (PR " + pr + ")"
	case branch != "":
		return " (branch " + branch + ")"
	}
	return ""
}

func loc(f findings.Finding) string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// gather collects issues and to-review hotspots, normalises and ranks them.
func (a *app) gather(ctx context.Context, r *sonarcli.Runner, s scopeFlags) (FindingsResult, error) {
	res := FindingsResult{Branch: s.branch, PullRequest: s.pr}
	if s.branch != "" && s.pr != "" {
		return res, usageErr("--branch and --pr cannot be combined")
	}
	minSev := findings.SevUnknown
	if s.minSeverity != "" {
		v, err := findings.ParseSeverity(s.minSeverity)
		if err != nil {
			return res, &ExitError{Code: ExitUsage, Err: err}
		}
		minSev = v
	}
	project := s.project
	if project == "" {
		project = a.cfg.DefaultProject
	}
	opts := sonarcli.ListIssuesOptions{Project: project, Branch: s.branch, PullRequest: s.pr, File: s.file, NewCode: s.newCode}
	issues, comps, det, err := r.ListIssues(ctx, opts, 0)
	if err != nil {
		var se *sonarcli.Error
		if errors.As(err, &se) && strings.Contains(se.Stderr, "Could not determine project key") {
			return res, usageErr("could not determine the project key: pass --project, set default_project in config.yaml, or run `sonar link <projectKey>` in the repository")
		}
		return res, err
	}
	if project == "" {
		project = det.Project
	}
	if res.PullRequest == "" && res.Branch == "" {
		res.PullRequest = det.PullRequest
	}
	if project == "" {
		return res, usageErr("could not determine the project key: pass --project, set default_project in config.yaml, or run `sonar link`")
	}
	res.Project = project
	// On a pull request everything is new code; otherwise ask the server
	// which findings are on new code.
	allNew := s.newCode || res.PullRequest != ""
	newIssues := map[string]bool{}
	if !allNew && len(issues) > 0 {
		o := opts
		o.Project, o.PullRequest, o.NewCode = project, res.PullRequest, true
		ni, _, _, err := r.ListIssues(ctx, o, 0)
		if err != nil {
			return res, err
		}
		for _, i := range ni {
			newIssues[i.Key] = true
		}
	}
	var all []findings.Finding
	for _, i := range issues {
		all = append(all, findings.FromIssue(i, comps, allNew || newIssues[i.Key]))
	}

	hopts := sonarcli.SearchHotspotsOptions{Project: project, Branch: s.branch, PullRequest: res.PullRequest, NewCode: s.newCode, Status: "TO_REVIEW"}
	if s.file != "" {
		hopts.Files = []string{s.file}
	}
	hs, hcomps, err := r.SearchHotspots(ctx, hopts, 0)
	if err != nil {
		return res, err
	}
	newHot := map[string]bool{}
	if !allNew && len(hs) > 0 {
		o := hopts
		o.NewCode = true
		nh, _, err := r.SearchHotspots(ctx, o, 0)
		if err != nil {
			return res, err
		}
		for _, h := range nh {
			newHot[h.Key] = true
		}
	}
	for _, h := range hs {
		all = append(all, findings.FromHotspot(h, hcomps, allNew || newHot[h.Key]))
	}
	all = findings.Filter{MinSeverity: minSev, SecurityOnly: s.securityOnly}.Apply(all)
	findings.Rank(all)
	if all == nil {
		all = []findings.Finding{}
	}
	res.Findings = all
	res.Total = len(all)
	res.Shown = len(all)
	return res, nil
}

func (a *app) findingsCmd() *cobra.Command {
	var s scopeFlags
	var limit int
	var failOn string
	cmd := &cobra.Command{
		Use:   "findings",
		Short: "List open issues and to-review hotspots, ranked security first",
		Long: `Lists open issues and security hotspots to review, ranked:
  1. vulnerabilities (SECURITY / VULNERABILITY)
  2. security hotspots to review (by vulnerability probability)
  3. reliability (RELIABILITY / BUG)
  4. maintainability (MAINTAINABILITY / CODE_SMELL)
then by severity, new code first, file and line.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var failSev findings.Severity
			if failOn != "" {
				v, err := findings.ParseSeverity(failOn)
				if err != nil {
					return &ExitError{Code: ExitUsage, Err: err}
				}
				failSev = v
			}
			if limit < 0 {
				return usageErr("--limit must be >= 0")
			}
			r, err := a.runner()
			if err != nil {
				return err
			}
			res, err := a.gather(cmd.Context(), r, s)
			if err != nil {
				return err
			}
			if limit > 0 && len(res.Findings) > limit {
				res.Findings = res.Findings[:limit]
				res.Shown = limit
			}
			if err := a.out(res); err != nil {
				return err
			}
			if failOn != "" && findings.AnyAtLeast(res.Findings[:res.Shown], failSev) {
				return &ExitError{Code: ExitFindings, Err: fmt.Errorf("findings at or above %s", failSev)}
			}
			return nil
		},
	}
	s.register(cmd)
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum findings to show (0 = all)")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 1 if any shown finding has at least this severity")
	return cmd
}

// NextResult is the output of `sqa next`.
type NextResult struct {
	Project   string            `json:"project"`
	Remaining int               `json:"remaining"`
	Finding   *findings.Finding `json:"finding"`
	Hint      string            `json:"hint"`
}

// Markdown renders the next finding.
func (n NextResult) Markdown() string { return n.Table() }

// Table renders the next finding.
func (n NextResult) Table() string {
	if n.Finding == nil {
		return "No open findings in " + n.Project + "."
	}
	f := n.Finding
	return fmt.Sprintf("[%d of %d] %s · %s · %s  (key: %s, %s)\nWhere: %s  (new code: %s)\nWhat:  %s\n\n%s",
		f.Rank, n.Remaining, f.Category, f.SeverityStr, f.Rule, f.Key, f.Kind, loc(*f), yesNo(f.NewCode), f.Message, n.Hint)
}

func (a *app) nextCmd() *cobra.Command {
	var s scopeFlags
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the single highest-priority open finding",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.runner()
			if err != nil {
				return err
			}
			res, err := a.gather(cmd.Context(), r, s)
			if err != nil {
				return err
			}
			n := NextResult{Project: res.Project, Remaining: res.Total}
			if len(res.Findings) > 0 {
				f := res.Findings[0]
				n.Finding = &f
				n.Hint = "Run `sqa show " + f.Key + "`, then present it to the user with a recommended verdict and ask for approval before acting."
			}
			return a.out(n)
		},
	}
	s.register(cmd)
	return cmd
}
