package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/findings"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// CodeLine is one line of code context.
type CodeLine struct {
	Line  int    `json:"line"`
	Focus bool   `json:"focus"`
	Code  string `json:"code"`
}

// FlowStep is one data-flow / secondary location.
type FlowStep struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Msg  string `json:"msg,omitempty"`
}

// Brief is the agent-ready description printed by `sqa show`.
type Brief struct {
	Key         string     `json:"key"`
	Kind        string     `json:"kind"`
	Category    string     `json:"category"`
	Severity    string     `json:"severity"`
	Quality     string     `json:"quality"`
	Status      string     `json:"status"`
	Rule        string     `json:"rule"`
	RuleName    string     `json:"rule_name,omitempty"`
	Language    string     `json:"language,omitempty"`
	File        string     `json:"file"`
	Line        int        `json:"line"`
	EndLine     int        `json:"end_line,omitempty"`
	Message     string     `json:"message"`
	Why         string     `json:"why,omitempty"`
	Assess      string     `json:"assess,omitempty"`
	HowToFix    string     `json:"how_to_fix,omitempty"`
	Code        []CodeLine `json:"code,omitempty"`
	Flows       []FlowStep `json:"flows,omitempty"`
	Verdicts    []string   `json:"verdicts"`
	NextStep    string     `json:"next_step"`
	ContextNote string     `json:"context_note,omitempty"`
}

// Markdown renders the brief.
func (b Brief) Markdown() string {
	var s strings.Builder
	fmt.Fprintf(&s, "## %s · %s · %s\n\n", b.Category, b.Severity, b.Rule)
	fmt.Fprintf(&s, "- **Key:** `%s` (%s, status %s)\n- **Rule:** %s\n- **Where:** `%s:%d`\n- **Message:** %s\n\n", b.Key, b.Kind, b.Status, b.RuleName, b.File, b.Line, b.Message)
	if len(b.Code) > 0 {
		s.WriteString("```\n")
		for _, c := range b.Code {
			mark := "  "
			if c.Focus {
				mark = "> "
			}
			fmt.Fprintf(&s, "%s%5d  %s\n", mark, c.Line, c.Code)
		}
		s.WriteString("```\n\n")
	}
	for _, sec := range []struct{ title, body string }{{"Why it matters", b.Why}, {"How to assess", b.Assess}, {"How to fix", b.HowToFix}} {
		if sec.body != "" {
			fmt.Fprintf(&s, "### %s\n\n%s\n\n", sec.title, sec.body)
		}
	}
	if len(b.Flows) > 0 {
		s.WriteString("### Flow\n\n")
		for i, f := range b.Flows {
			fmt.Fprintf(&s, "%d. `%s:%d` %s\n", i+1, f.File, f.Line, f.Msg)
		}
		s.WriteString("\n")
	}
	fmt.Fprintf(&s, "**Possible verdicts:** %s\n\n**Next step:** %s\n", strings.Join(b.Verdicts, ", "), b.NextStep)
	return s.String()
}

// Table renders the brief for a terminal.
func (b Brief) Table() string { return b.Markdown() }

const maxSection = 6000

func clipSection(s string) string {
	if len(s) <= maxSection {
		return s
	}
	return s[:maxSection] + "\n…(truncated)"
}

func ruleSections(rule *sonarcli.Rule, ctxKey string) (why, assess, fix string) {
	if rule == nil {
		return "", "", ""
	}
	parts := map[string][]string{}
	for _, sec := range rule.DescriptionSections {
		if sec.Context != nil && ctxKey != "" && sec.Context.Key != ctxKey {
			continue
		}
		body := sonarcli.HTMLToText(sec.Content)
		if sec.Context != nil && ctxKey == "" {
			body = "[" + sec.Context.DisplayName + "]\n" + body
		}
		parts[sec.Key] = append(parts[sec.Key], body)
	}
	join := func(keys ...string) string {
		var out []string
		for _, k := range keys {
			out = append(out, parts[k]...)
		}
		return clipSection(strings.TrimSpace(strings.Join(out, "\n\n")))
	}
	why = join("introduction", "root_cause", "default")
	if why == "" {
		if rule.HTMLDesc != "" {
			why = clipSection(sonarcli.HTMLToText(rule.HTMLDesc))
		} else {
			why = clipSection(rule.MDDesc)
		}
	}
	return why, join("assess_the_problem"), join("how_to_fix")
}

func flowSteps(flows []sonarcli.Flow, comps []sonarcli.Component) []FlowStep {
	var out []FlowStep
	for _, f := range flows {
		for _, l := range f.Locations {
			st := FlowStep{File: sonarcli.FilePath(l.Component, comps), Msg: l.Msg}
			if l.TextRange != nil {
				st.Line = l.TextRange.StartLine
			}
			out = append(out, st)
		}
	}
	return out
}

func (a *app) showCmd() *cobra.Command {
	var branch, pr string
	var contextLines int
	cmd := &cobra.Command{
		Use:   "show <key>",
		Short: "Agent-ready brief for one issue or hotspot: rule, rationale, code context, fix guidance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			key := args[0]
			if contextLines < 0 || contextLines > 50 {
				return usageErr("--context must be between 0 and 50")
			}
			r, err := a.runner()
			if err != nil {
				return err
			}
			var b Brief
			var component string
			var tr *sonarcli.TextRange
			var rule *sonarcli.Rule

			issue, comps, err := r.GetIssue(ctx, key)
			if err != nil {
				return err
			}
			if issue != nil {
				f := findings.FromIssue(*issue, comps, false)
				b = Brief{Key: key, Kind: string(f.Kind), Category: f.Category, Severity: f.SeverityStr, Quality: f.Quality,
					Status: f.Status, Rule: issue.Rule, File: f.File, Line: f.Line, Message: issue.Message,
					Flows: flowSteps(issue.Flows, comps), Verdicts: []string{"fix", "fp", "accept"}}
				component, tr = issue.Component, issue.TextRange
				rule, _ = r.ShowRule(ctx, issue.Rule)
				b.Why, b.Assess, b.HowToFix = ruleSections(rule, issue.RuleDescriptionCtx)
			} else {
				h, err := r.ShowHotspot(ctx, key)
				if err != nil {
					var se *sonarcli.Error
					if errors.As(err, &se) && strings.Contains(se.Stderr, "404") {
						return usageErr("no issue or hotspot with key %q", key)
					}
					return err
				}
				f := findings.FromHotspot(sonarcli.Hotspot{Key: h.Key, Component: h.Component.Key, RuleKey: h.Rule.Key,
					VulnerabilityProbability: h.Rule.VulnerabilityProbability, Status: h.Status, Line: h.Line, Message: h.Message, TextRange: h.TextRange},
					[]sonarcli.Component{h.Component}, false)
				b = Brief{Key: key, Kind: string(f.Kind), Category: f.Category, Severity: f.SeverityStr, Quality: f.Quality,
					Status: h.Status, Rule: h.Rule.Key, RuleName: h.Rule.Name, File: f.File, Line: f.Line, Message: h.Message,
					Flows: flowSteps(h.Flows, []sonarcli.Component{h.Component}), Verdicts: []string{"fix (then mark fixed)", "safe", "acknowledged"}}
				if h.Resolution != "" {
					b.Status += "/" + h.Resolution
				}
				component, tr = h.Component.Key, h.TextRange
				rule, _ = r.ShowRule(ctx, h.Rule.Key)
				b.Why, b.Assess, b.HowToFix = ruleSections(rule, "")
				if b.Why == "" {
					b.Why = clipSection(sonarcli.HTMLToText(h.Rule.RiskDescription + "\n\n" + h.Rule.VulnerabilityDescription))
				}
				if b.HowToFix == "" {
					b.HowToFix = clipSection(sonarcli.HTMLToText(h.Rule.FixRecommendations))
				}
			}
			if rule != nil {
				b.RuleName, b.Language = rule.Name, rule.LangName
			}
			start, end := b.Line, b.Line
			if tr != nil {
				start, end = tr.StartLine, tr.EndLine
				b.EndLine = tr.EndLine
			}
			if start > 0 {
				from := max(1, start-contextLines)
				lines, err := r.SourceLines(ctx, component, from, end+contextLines, branch, pr)
				if err != nil {
					b.ContextNote = "code context unavailable: " + err.Error()
				}
				for _, l := range lines {
					b.Code = append(b.Code, CodeLine{Line: l.Line, Focus: l.Line >= start && l.Line <= end, Code: sonarcli.PlainText(l.Code)})
				}
			}
			b.NextStep = "Present this finding to the user with your recommended verdict and reasoning, then STOP and ask for explicit approval. Only after approval: fix the code, or run `sqa mark`/`sqa hotspot` with --approved-by-user and the user's reasoning as --comment."
			return a.out(b)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch (for code context)")
	cmd.Flags().StringVar(&pr, "pr", "", "pull request id (for code context)")
	cmd.Flags().IntVar(&contextLines, "context", 5, "lines of code context around the finding")
	return cmd
}
