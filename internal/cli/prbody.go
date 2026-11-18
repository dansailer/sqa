package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/findings"
	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// PRBodyItem is one finding listed in a PR body.
type PRBodyItem struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Rule     string `json:"rule"`
	RuleName string `json:"rule_name"`
	Message  string `json:"message"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	URL      string `json:"url"`
}

// PRBody is the output of `sqa pr-body`.
type PRBody struct {
	Project  string       `json:"project,omitempty"`
	Reason   string       `json:"reason,omitempty"`
	Findings []PRBodyItem `json:"findings"`
	Body     string       `json:"markdown"`
}

// Markdown returns the block to paste into the PR body.
func (p PRBody) Markdown() string { return p.Body }

// Table prints the same block.
func (p PRBody) Table() string { return p.Body }

// mdLinkText escapes characters that would end a Markdown link label.
func mdLinkText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, "\n", " ")
	return r.Replace(s)
}

func renderPRBody(items []PRBodyItem, reason string) string {
	var b strings.Builder
	b.WriteString("### Resolves SonarQube findings\n\n")
	if reason != "" {
		fmt.Fprintf(&b, "Resolved by: %s\n\n", strings.TrimSpace(reason))
	}
	for _, it := range items {
		label := mdLinkText(it.Message)
		if it.URL != "" {
			fmt.Fprintf(&b, "- [%s](%s)", label, it.URL)
		} else {
			fmt.Fprintf(&b, "- %s", label)
		}
		rule := "`" + it.Rule + "`"
		if it.RuleName != "" {
			rule += " " + it.RuleName
		}
		loc := it.File
		if it.Line > 0 {
			loc = fmt.Sprintf("%s:%d", it.File, it.Line)
		}
		// The key stays in plain text so open-PR detection finds it even if
		// the link is edited away.
		fmt.Fprintf(&b, " (%s · `%s` · key `%s`)\n", rule, loc, it.Key)
	}
	return b.String()
}

type prBodyBuilder struct {
	ctx       context.Context
	r         *sonarcli.Runner
	server    string
	ruleNames map[string]string
	branch    string
	pr        string
}

func (pb *prBodyBuilder) ruleName(key string) string {
	if n, ok := pb.ruleNames[key]; ok {
		return n
	}
	name := ""
	if rule, err := pb.r.ShowRule(pb.ctx, key); err == nil && rule != nil {
		name = rule.Name
	}
	pb.ruleNames[key] = name
	return name
}

func (pb *prBodyBuilder) fromFinding(f findings.Finding, project string) PRBodyItem {
	return PRBodyItem{Key: f.Key, Kind: string(f.Kind), Rule: f.Rule, RuleName: pb.ruleName(f.Rule), Message: f.Message,
		File: f.File, Line: f.Line, URL: FindingURL(pb.server, project, f.Key, string(f.Kind), pb.branch, pb.pr)}
}

// fromKey looks a key up as an issue, then as a hotspot.
func (pb *prBodyBuilder) fromKey(key string) (PRBodyItem, error) {
	issue, comps, err := pb.r.GetIssue(pb.ctx, key, pb.branch, pb.pr)
	if err != nil {
		return PRBodyItem{}, err
	}
	if issue != nil {
		return pb.fromFinding(findings.FromIssue(*issue, comps, false), issue.Project), nil
	}
	h, err := pb.r.ShowHotspot(pb.ctx, key)
	if err != nil {
		if isNotFound(err) {
			return PRBodyItem{}, usageErr("no issue or hotspot with key %q", key)
		}
		return PRBodyItem{}, err
	}
	f := findings.FromHotspot(sonarcli.Hotspot{Key: h.Key, Component: h.Component.Key, RuleKey: h.Rule.Key, Line: h.Line,
		Message: h.Message, TextRange: h.TextRange}, []sonarcli.Component{h.Component}, false)
	it := pb.fromFinding(f, h.Project.Key)
	if it.RuleName == "" {
		it.RuleName = h.Rule.Name
	}
	return it, nil
}

func (a *app) prBodyCmd() *cobra.Command {
	var s scopeFlags
	var fromFindings bool
	var reason string
	cmd := &cobra.Command{
		Use:   "pr-body [<key>...]",
		Short: "Print the 'Resolves SonarQube findings' Markdown block for a PR body",
		Long: `Prints a Markdown block listing the findings a PR resolves, one line each:
message (linked to the finding in SonarQube), rule key and name, location and
the finding key, so sqa's open-PR check matches every listed finding.

Give the keys as arguments, or use --from-findings with the usual filters
(--file accepts a path or a glob such as 'test/**') to list every finding a
config change such as a Sonar exclusion will remove. --reason adds a
"Resolved by:" line, e.g. "excluding test fixtures via sonar.exclusions".

Read-only. Prints Markdown unless --format is given.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !fromFindings {
				return usageErr("give finding keys or --from-findings")
			}
			if len(args) > 0 && fromFindings {
				return usageErr("give finding keys or --from-findings, not both")
			}
			r, err := a.runner()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			pb := &prBodyBuilder{ctx: ctx, r: r, ruleNames: map[string]string{}, branch: s.branch, pr: s.pr}
			if st, err := r.AuthStatus(ctx); err == nil {
				pb.server = st.Server
			}
			res := PRBody{Reason: strings.TrimSpace(reason), Findings: []PRBodyItem{}}
			if fromFindings {
				g, err := a.gather(ctx, r, s)
				if err != nil {
					return err
				}
				res.Project = g.Project
				for _, f := range g.Findings {
					res.Findings = append(res.Findings, pb.fromFinding(f, g.Project))
				}
			} else {
				seen := map[string]bool{}
				for _, k := range args {
					k = strings.TrimSpace(k)
					if k == "" || seen[k] {
						continue
					}
					seen[k] = true
					it, err := pb.fromKey(k)
					if err != nil {
						return err
					}
					res.Findings = append(res.Findings, it)
				}
			}
			if len(res.Findings) == 0 {
				return usageErr("no findings match; nothing to list")
			}
			res.Body = renderPRBody(res.Findings, res.Reason)
			if a.format == "" {
				return output.Write(a.stdout, output.FormatMD, res)
			}
			return a.out(res)
		},
	}
	s.register(cmd)
	cmd.Flags().BoolVar(&fromFindings, "from-findings", false, "list every finding matching the filters instead of named keys")
	cmd.Flags().StringVar(&reason, "reason", "", "add a 'Resolved by:' line (e.g. the exclusion that removes these findings)")
	return cmd
}
