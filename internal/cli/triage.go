package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/approval"
	"github.com/dansailer/sqa/internal/config"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// ChangeResult reports a (possibly dry-run) state change.
type ChangeResult struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Action    string `json:"action"`
	APICall   string `json:"api_call"`
	Comment   string `json:"comment"`
	DryRun    bool   `json:"dry_run"`
	Result    string `json:"result"`
	AuditFile string `json:"audit_file,omitempty"`
}

// Markdown renders the change.
func (c ChangeResult) Markdown() string { return c.Table() }

// Table renders the change.
func (c ChangeResult) Table() string {
	verb := "changed"
	if c.DryRun {
		verb = "would change (dry run)"
	}
	return fmt.Sprintf("%s %s %s: %s\nAPI:     %s\ncomment: %s", c.Kind, c.Key, verb, c.Action, c.APICall, c.Comment)
}

func verbs(m map[string]string) string {
	k := make([]string, 0, len(m))
	for v := range m {
		k = append(k, v)
	}
	sort.Strings(k)
	return strings.Join(k, "|")
}

type changeFlags struct {
	comment  string
	approved bool
	dryRun   bool
}

func (c *changeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.comment, "comment", "", "justification, reflecting the user's reasoning (required; posted to SonarQube prefixed with [sqa])")
	cmd.Flags().BoolVar(&c.approved, "approved-by-user", false, "confirm the user explicitly approved this exact change (required)")
	cmd.Flags().BoolVar(&c.dryRun, "dry-run", false, "show what would change without calling the server")
}

// change is one approved state change: what it does and how to do it.
type change struct {
	kind, action, apiCall string
	do                    func(ctx context.Context, r *sonarcli.Runner, comment string) error
}

// apply checks the approval guardrails, runs ch (unless --dry-run), writes
// the audit line and prints the result.
func (a *app) apply(ctx context.Context, key string, c changeFlags, ch change) error {
	if err := approval.Check(c.approved, c.comment); err != nil {
		return err
	}
	res := ChangeResult{Key: key, Kind: ch.kind, Action: ch.action, APICall: ch.apiCall, Comment: approval.ServerComment(c.comment), DryRun: c.dryRun}
	entry := approval.NewEntry(res.Kind, res.Key, res.Action, res.Comment)
	entry.DryRun = res.DryRun
	var opErr error
	switch {
	case res.DryRun:
		res.Result = "dry-run"
	default:
		if r, err := a.runner(); err != nil {
			opErr = err
		} else {
			opErr = ch.do(ctx, r, res.Comment)
		}
		res.Result = "ok"
		if opErr != nil {
			res.Result, entry.ErrorMsg = "error", opErr.Error()
		}
	}
	entry.Result = res.Result
	if p, err := a.audit(entry); err != nil {
		_, _ = fmt.Fprintf(a.stderr, "sqa: warning: could not write audit log: %v\n", err)
	} else {
		res.AuditFile = p
	}
	if opErr != nil {
		return opErr
	}
	return a.out(res)
}

func (a *app) audit(e approval.Entry) (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "audit.jsonl")
	return p, approval.Append(p, e)
}

func (a *app) markCmd() *cobra.Command {
	var c changeFlags
	cmd := &cobra.Command{
		Use:   "mark <issue-key> " + verbs(approval.IssueTransitions),
		Short: "Mark an issue false positive / accepted / reopened (requires --approved-by-user and --comment)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, verb := args[0], strings.ToLower(args[1])
			transition, ok := approval.IssueTransitions[verb]
			if !ok {
				return usageErr("unknown verdict %q (use %s)", args[1], verbs(approval.IssueTransitions))
			}
			return a.apply(cmd.Context(), key, c, change{kind: "issue", action: verb,
				apiCall: "POST /api/issues/do_transition transition=" + transition + " + POST /api/issues/add_comment",
				do: func(ctx context.Context, r *sonarcli.Runner, comment string) error {
					if err := r.TransitionIssue(ctx, key, transition); err != nil {
						return err
					}
					if err := r.CommentIssue(ctx, key, comment); err != nil {
						// Do not let a retry repeat the transition.
						return fmt.Errorf("transition %s was applied, but adding the comment failed (add it in SonarQube): %w", transition, err)
					}
					return nil
				}})
		},
	}
	c.register(cmd)
	return cmd
}

// hotspotVerbs lists every `sqa hotspot` action for help and errors.
func hotspotVerbs() string {
	return verbs(approval.HotspotResolutions) + "|reopen|comment|assign <login>"
}

// hotspotChange maps `sqa hotspot <key> <verb> [login]` to a change.
func hotspotChange(key string, args []string) (change, error) {
	verb := strings.ToLower(args[0])
	ch := change{kind: "hotspot", action: verb}
	switch {
	case verb == "assign":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return ch, usageErr("assign needs a login: sqa hotspot <key> assign <login>")
		}
		login := strings.TrimSpace(args[1])
		ch.action, ch.apiCall = "assign "+login, "POST /api/hotspots/assign assignee="+login
		ch.do = func(ctx context.Context, r *sonarcli.Runner, comment string) error {
			return r.AssignHotspot(ctx, key, login, comment)
		}
	case len(args) == 2:
		return ch, usageErr("only assign takes a third argument")
	case verb == "reopen":
		ch.apiCall = "POST /api/hotspots/change_status status=TO_REVIEW"
		ch.do = func(ctx context.Context, r *sonarcli.Runner, comment string) error {
			return r.ReopenHotspot(ctx, key, comment)
		}
	case verb == "comment":
		ch.apiCall = "POST /api/hotspots/add_comment"
		ch.do = func(ctx context.Context, r *sonarcli.Runner, comment string) error {
			return r.CommentHotspot(ctx, key, comment)
		}
	default:
		resolution, ok := approval.HotspotResolutions[verb]
		if !ok {
			return ch, usageErr("unknown action %q (use %s)", args[0], hotspotVerbs())
		}
		ch.apiCall = "POST /api/hotspots/change_status status=REVIEWED resolution=" + resolution
		ch.do = func(ctx context.Context, r *sonarcli.Runner, comment string) error {
			return r.ReviewHotspot(ctx, key, resolution, comment)
		}
	}
	return ch, nil
}

func (a *app) hotspotCmd() *cobra.Command {
	var c changeFlags
	cmd := &cobra.Command{
		Use:   "hotspot <hotspot-key> " + hotspotVerbs(),
		Short: "Review, reopen, comment on or assign a security hotspot (requires --approved-by-user and --comment)",
		Long: `Changes a security hotspot in SonarQube, only after the user approved this exact change:

  safe|fixed|acknowledged  review it (status REVIEWED with that resolution)
  reopen                   set it back to TO_REVIEW
  comment                  add --comment without changing anything else
  assign <login>           assign it to a SonarQube user (TO_REVIEW or ACKNOWLEDGED only)

Every action needs --approved-by-user and a --comment (posted with an [sqa]
prefix), supports --dry-run and is written to the audit log.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch, err := hotspotChange(args[0], args[1:])
			if err != nil {
				return err
			}
			return a.apply(cmd.Context(), args[0], c, ch)
		},
	}
	c.register(cmd)
	return cmd
}
