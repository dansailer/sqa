package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/findings"
	"github.com/dansailer/sqa/internal/forge"
	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

type scopeFlags struct {
	project, branch, pr, file string
	newCode                   bool
	minSeverity               string
	securityOnly              bool
	noAcknowledged            bool
}

func (s *scopeFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&s.project, "project", "p", "", "project key (default: default_project from config, else sonar auto-detection)")
	f.StringVar(&s.branch, "branch", "", "branch name")
	f.StringVar(&s.pr, "pr", "", "pull request id")
	f.StringVar(&s.file, "file", "", "limit to one file (or a directory's own files), path from project root; or a glob such as 'test/**' or '**/*_test.go'")
	f.BoolVar(&s.newCode, "new-code", false, "only findings on new code")
	f.StringVar(&s.minSeverity, "min-severity", "", "minimum severity: BLOCKER, HIGH|CRITICAL, MEDIUM|MAJOR, LOW|MINOR, INFO")
	f.BoolVar(&s.securityOnly, "security-only", false, "only vulnerabilities and security hotspots")
	f.BoolVar(&s.noAcknowledged, "no-acknowledged", false, "hide hotspots reviewed as ACKNOWLEDGED (fix pending); shown by default")
}

// registerNoPRCheck adds --no-pr-check to the commands that look up open fix
// PRs (findings, next, show).
func registerNoPRCheck(cmd *cobra.Command, off *bool) {
	cmd.Flags().BoolVar(off, "no-pr-check", false, "do not look for open fix PRs on the forge (gh / git ls-remote)")
}

// FindingsResult is the output of `sqa findings`.
type FindingsResult struct {
	// Summary is the quality gate read-out (coverage, hotspots reviewed),
	// first so it is not missed; nil with --no-summary.
	Summary     *GateSummary `json:"summary,omitempty"`
	Project     string       `json:"project"`
	Branch      string       `json:"branch,omitempty"`
	PullRequest string       `json:"pull_request,omitempty"`
	Total       int          `json:"total"`
	Shown       int          `json:"shown"`
	// PRCheck says how open fix PRs were looked up: gh, git (remote
	// branches only), off (--no-pr-check), unavailable, or none (nothing to check).
	PRCheck  string             `json:"pr_check"`
	Note     string             `json:"note,omitempty"`
	Findings []findings.Finding `json:"findings"`
}

// Markdown renders a markdown table.
func (r FindingsResult) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## SonarQube findings: %s%s\n\n", r.Project, scopeSuffix(r.Branch, r.PullRequest))
	if t := r.Summary.text("- "); t != "" {
		b.WriteString("### Quality gate\n\n" + t + "\n")
	}
	fmt.Fprintf(&b, "Showing %d of %d, ranked security first.\n\n", r.Shown, r.Total)
	if r.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n\n", r.Note)
	}
	if len(r.Findings) == 0 {
		b.WriteString("No open findings.\n")
		return b.String()
	}
	b.WriteString("| # | category | severity | status | key | rule | location | new | PR | message |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", f.Rank, f.Category, f.Severity, f.Status, f.Key, f.Rule,
			output.MDEscape(loc(f)), yesNo(f.NewCode), output.MDEscape(f.PR), output.MDEscape(f.Message))
	}
	return b.String()
}

// Table renders a terminal table.
func (r FindingsResult) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s: %d of %d findings (security first)\n", r.Project, scopeSuffix(r.Branch, r.PullRequest), r.Shown, r.Total)
	b.WriteString(r.Summary.text("  "))
	if r.Note != "" {
		fmt.Fprintf(&b, "note: %s\n", r.Note)
	}
	if len(r.Findings) == 0 {
		return b.String()
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%-4s %-15s %-8s %-12s %-3s %-22s %-40s %-8s %s\n", "#", "CATEGORY", "SEVERITY", "STATUS", "NEW", "KEY", "LOCATION", "PR", "MESSAGE")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%-4d %-15s %-8s %-12s %-3s %-22s %-40s %-8s %s\n", f.Rank, f.Category, f.Severity, shortStatus(f.Status), yesNo(f.NewCode), f.Key, clip(loc(f), 40),
			shortPR(f.PR), clip(f.Message, 90))
	}
	return b.String()
}

// shortStatus drops the REVIEWED/ prefix of an acknowledged hotspot.
func shortStatus(s string) string { return strings.TrimPrefix(s, "REVIEWED/") }

// shortPR renders a PR reference for the terminal table: `#12` for a
// GitHub-style URL, `branch` for a remote fix branch, else the reference.
func shortPR(ref string) string {
	if ref == "" {
		return "-"
	}
	if strings.HasPrefix(ref, "branch:") {
		return "branch"
	}
	if i := strings.LastIndex(ref, "/pull/"); i >= 0 {
		return "#" + ref[i+len("/pull/"):]
	}
	if i := strings.LastIndex(ref, "/merge_requests/"); i >= 0 {
		return "!" + ref[i+len("/merge_requests/"):]
	}
	return clip(ref, 8)
}

// prIndex looks up open fix PRs once per invocation, unless disabled.
func prIndex(ctx context.Context, off bool) (forge.Index, string) {
	if off {
		return forge.Index{}, "off"
	}
	ix := forge.Detect(ctx, "")
	if !ix.Available() {
		return ix, "unavailable"
	}
	return ix, ix.Source
}

// projectPath normalises a --file value to SonarQube's form: forward
// slashes, no leading "./" and no leading or trailing "/".
func projectPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return strings.Trim(p, "/")
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
// It only talks to SonarQube; markOpenPRs adds the forge lookup.
func (a *app) gather(ctx context.Context, r *sonarcli.Runner, s scopeFlags) (FindingsResult, error) {
	res := FindingsResult{Branch: s.branch, PullRequest: s.pr}
	minSev := findings.SevUnknown
	if s.minSeverity != "" {
		v, err := findings.ParseSeverity(s.minSeverity)
		if err != nil {
			return res, &ExitError{Code: ExitUsage, Err: err}
		}
		minSev = v
	}
	sc, err := a.resolveScope(ctx, r, s.project, s.branch, s.pr)
	if err != nil {
		return res, err
	}
	listBranch, err := sc.listBranch(ctx, r)
	if err != nil {
		return res, err
	}
	project := sc.project
	res.Project = project
	// A glob is matched by sqa on the full list; a plain path goes to the
	// server (sonar's --file: a file, or a directory's own files).
	serverFile := projectPath(s.file)
	var match func(string) bool
	if findings.IsGlob(serverFile) {
		m, err := findings.CompileGlob(serverFile)
		if err != nil {
			return res, usageErr("bad --file glob %q: %v", s.file, err)
		}
		serverFile, match = "", m
	}
	opts := sonarcli.ListIssuesOptions{Project: project, Branch: listBranch, PullRequest: s.pr, File: serverFile, NewCode: s.newCode}
	issues, comps, _, err := r.ListIssues(ctx, opts, 0)
	if err != nil {
		return res, err
	}
	// On a pull request everything is new code; otherwise ask the server
	// which findings are on new code.
	allNew := s.newCode || s.pr != ""
	newIssues := map[string]bool{}
	if !allNew && len(issues) > 0 {
		o := opts
		o.NewCode = true
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

	hopts := sonarcli.SearchHotspotsOptions{Project: project, Branch: s.branch, PullRequest: s.pr, NewCode: s.newCode, Status: "TO_REVIEW"}
	if serverFile != "" {
		hopts.Files = []string{serverFile}
	}
	hs, hcomps, err := r.SearchHotspots(ctx, hopts, 0)
	if err != nil {
		return res, err
	}
	searches := []sonarcli.SearchHotspotsOptions{hopts}
	if !s.noAcknowledged {
		// Reviewed as ACKNOWLEDGED means "risk confirmed, fix pending": keep it
		// in the queue so it does not vanish once someone acknowledged it.
		ack := hopts
		ack.Status, ack.Resolution = "REVIEWED", "ACKNOWLEDGED"
		ah, acomps, err := r.SearchHotspots(ctx, ack, 0)
		if err != nil {
			return res, err
		}
		if len(ah) > 0 {
			hs, hcomps = append(hs, ah...), append(hcomps, acomps...)
			searches = append(searches, ack)
		}
	}
	newHot := map[string]bool{}
	if !allNew && len(hs) > 0 {
		for _, o := range searches {
			o.NewCode = true
			nh, _, err := r.SearchHotspots(ctx, o, 0)
			if err != nil {
				return res, err
			}
			for _, h := range nh {
				newHot[h.Key] = true
			}
		}
	}
	for _, h := range hs {
		all = append(all, findings.FromHotspot(h, hcomps, allNew || newHot[h.Key]))
	}
	all = findings.Filter{MinSeverity: minSev, SecurityOnly: s.securityOnly, Path: match}.Apply(all)
	findings.Rank(all)
	if all == nil {
		all = []findings.Finding{}
	}
	res.Findings = all
	res.Total = len(all)
	res.Shown = len(all)
	return res, nil
}

// markOpenPRs looks up open fix PRs once (unless off) and sets each
// finding's PR, plus PRCheck and Note on res.
func markOpenPRs(ctx context.Context, res *FindingsResult, off bool) {
	if len(res.Findings) == 0 && !off {
		res.PRCheck = "none"
		return
	}
	ix, check := prIndex(ctx, off)
	res.PRCheck, res.Note = check, ix.Note
	for i := range res.Findings {
		if p, ok := ix.Lookup(res.Findings[i].Key); ok {
			res.Findings[i].PR = p.Ref()
		}
	}
}

func (a *app) findingsCmd() *cobra.Command {
	var s scopeFlags
	var noPRCheck bool
	var limit int
	var failOn string
	var noSummary bool
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
			markOpenPRs(cmd.Context(), &res, noPRCheck)
			if limit > 0 && len(res.Findings) > limit {
				res.Findings = res.Findings[:limit]
				res.Shown = limit
			}
			if !noSummary {
				res.Summary = gateSummary(cmd.Context(), r, res.Project, s.branch, s.pr)
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
	registerNoPRCheck(cmd, &noPRCheck)
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum findings to show (0 = all)")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 1 if any shown finding has at least this severity")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "skip the quality gate summary (coverage, hotspots reviewed) at the end")
	return cmd
}

// Skipped is a finding `sqa next` passed over.
type Skipped struct {
	Rank   int    `json:"rank"`
	Key    string `json:"key"`
	Reason string `json:"reason"` // "open PR" or "--skip"
	PR     string `json:"pr"`
}

// NextResult is the output of `sqa next`.
type NextResult struct {
	// Gate is the quality gate read-out, first so it is not missed; nil
	// with --no-summary.
	Gate      *GateSummary      `json:"gate,omitempty"`
	Project   string            `json:"project"`
	Total     int               `json:"total"`
	Remaining int               `json:"remaining"`
	PRCheck   string            `json:"pr_check"`
	Note      string            `json:"note,omitempty"`
	Skipped   []Skipped         `json:"skipped"`
	Finding   *findings.Finding `json:"finding"`
	Brief     *Brief            `json:"brief,omitempty"`
	Hint      string            `json:"hint"`
}

// Table renders a rank header, then the same brief as `sqa show`.
func (n NextResult) Table() string {
	var b strings.Builder
	b.WriteString(n.Gate.text(""))
	if len(n.Skipped) > 0 {
		var parts []string
		for _, sk := range n.Skipped {
			p := sk.Key + " (" + sk.Reason
			if sk.PR != "" {
				p += " " + sk.PR
			}
			parts = append(parts, p+")")
		}
		fmt.Fprintf(&b, "Skipped %d: %s\n", len(n.Skipped), strings.Join(parts, ", "))
	}
	if n.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", n.Note)
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	if n.Finding == nil {
		if len(n.Skipped) > 0 {
			b.WriteString("No other open findings in " + n.Project + ".")
		} else {
			b.WriteString("No open findings in " + n.Project + ".")
		}
		if n.Hint != "" {
			b.WriteString("\n" + n.Hint)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "[%d of %d] %s  (new code: %s, %d remaining)", n.Finding.Rank, n.Total, n.Project, yesNo(n.Finding.NewCode), n.Remaining)
	if n.Brief != nil {
		b.WriteString("\n\n" + n.Brief.Markdown())
	}
	return b.String()
}

// pickNext returns the first finding that is neither in skip nor (unless
// includeOpenPR) has an open PR, and the findings passed over before it.
func pickNext(fs []findings.Finding, skip map[string]bool, includeOpenPR bool) (*findings.Finding, []Skipped, int) {
	var skipped []Skipped
	var next *findings.Finding
	remaining := 0
	for i := range fs {
		f := fs[i]
		switch {
		case skip[f.Key]:
			skipped = append(skipped, Skipped{Rank: f.Rank, Key: f.Key, Reason: "--skip", PR: f.PR})
			continue
		case f.PR != "" && !includeOpenPR:
			skipped = append(skipped, Skipped{Rank: f.Rank, Key: f.Key, Reason: "open PR", PR: f.PR})
			continue
		}
		remaining++
		if next == nil {
			next = &f
		}
	}
	if skipped == nil {
		skipped = []Skipped{}
	}
	return next, skipped, remaining
}

func (a *app) nextCmd() *cobra.Command {
	var s scopeFlags
	var noPRCheck, noSummary bool
	var skip []string
	var includeOpenPR bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the highest-priority open finding without an open fix PR, with the full brief",
		Long: `Shows the highest-ranked finding with the full brief (same as sqa show).

Findings that already have an open fix PR are skipped. sqa looks them up once
per run with 'gh pr list' (a PR whose body contains the finding key or whose
head branch contains it, e.g. sonar/<rule>-<key>); without gh it falls back to
'git ls-remote --heads origin sonar/*'. If neither works, nothing is skipped
and a note says so. --include-open-pr turns skipping off, --no-pr-check turns
the lookup off, --skip <key> passes over a finding by hand (repeatable).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.runner()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			res, err := a.gather(ctx, r, s)
			if err != nil {
				return err
			}
			markOpenPRs(ctx, &res, noPRCheck)
			skipSet := map[string]bool{}
			for _, k := range skip {
				if k = strings.TrimSpace(k); k != "" {
					skipSet[k] = true
				}
			}
			f, skipped, remaining := pickNext(res.Findings, skipSet, includeOpenPR)
			n := NextResult{Project: res.Project, Total: res.Total, Remaining: remaining, PRCheck: res.PRCheck, Note: res.Note, Skipped: skipped}
			if f != nil {
				n.Finding = f
				b, err := loadBrief(ctx, r, f.Key, defaultContextLines, s.branch, s.pr)
				if err != nil {
					return err
				}
				b.PR = f.PR
				n.Brief = &b
				n.Hint = "Present this finding with a recommended verdict and ask the user before acting."
				if f.Status == "REVIEWED/ACKNOWLEDGED" {
					n.Hint = "This hotspot was reviewed as ACKNOWLEDGED (risk confirmed, fix pending): propose a fix, or ask whether to reopen or mark it safe. Ask the user before acting."
				}
				if f.PR != "" {
					n.Hint = "This finding already has an open fix PR (" + f.PR + "): tell the user and do not open another; ask how to proceed."
				}
			}
			if !noSummary {
				n.Gate = gateSummary(ctx, r, res.Project, s.branch, s.pr)
				if steps := strings.Join(n.Gate.NextSteps, " "); steps != "" {
					if n.Hint == "" {
						n.Hint = "No open findings left, but the quality gate is failing. " + steps
					} else {
						n.Hint += " Also: " + steps
					}
				}
			}
			return a.out(n)
		},
	}
	s.register(cmd)
	registerNoPRCheck(cmd, &noPRCheck)
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "finding key to pass over (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&includeOpenPR, "include-open-pr", false, "do not skip findings that already have an open fix PR")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "skip the quality gate read-out (coverage, hotspots reviewed)")
	return cmd
}
