package cli

import (
	"fmt"
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

func (a *app) audit(e approval.Entry) (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	p := dir + "/audit.jsonl"
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
			if err := approval.Check(c.approved, c.comment); err != nil {
				return err
			}
			comment := approval.ServerComment(c.comment)
			res := ChangeResult{Key: key, Kind: "issue", Action: verb, Comment: comment, DryRun: c.dryRun,
				APICall: "POST /api/issues/do_transition transition=" + transition + " + POST /api/issues/add_comment"}
			return a.applyChange(cmd, &res, func() error {
				r, err := a.runner()
				if err != nil {
					return err
				}
				if err := r.TransitionIssue(cmd.Context(), key, transition); err != nil {
					return err
				}
				if err := r.CommentIssue(cmd.Context(), key, comment); err != nil {
					// Do not let a retry repeat the transition.
					return fmt.Errorf("transition %s was applied, but adding the comment failed (add it in SonarQube): %w", transition, err)
				}
				return nil
			})
		},
	}
	c.register(cmd)
	return cmd
}

// hotspotVerbs lists every `sqa hotspot` action for help and errors.
func hotspotVerbs() string {
	return verbs(approval.HotspotResolutions) + "|reopen|comment|assign <login>"
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
			key, verb := args[0], strings.ToLower(args[1])
			var res ChangeResult
			var do func(r *sonarcli.Runner) error
			switch {
			case verb == "assign":
				if len(args) != 3 || strings.TrimSpace(args[2]) == "" {
					return usageErr("assign needs a login: sqa hotspot <key> assign <login>")
				}
				login := strings.TrimSpace(args[2])
				res = ChangeResult{Action: "assign " + login, APICall: "POST /api/hotspots/assign assignee=" + login}
				do = func(r *sonarcli.Runner) error { return r.AssignHotspot(cmd.Context(), key, login, res.Comment) }
			case len(args) == 3:
				return usageErr("only assign takes a third argument")
			case verb == "reopen":
				res = ChangeResult{Action: verb, APICall: "POST /api/hotspots/change_status status=TO_REVIEW"}
				do = func(r *sonarcli.Runner) error { return r.ReopenHotspot(cmd.Context(), key, res.Comment) }
			case verb == "comment":
				res = ChangeResult{Action: verb, APICall: "POST /api/hotspots/add_comment"}
				do = func(r *sonarcli.Runner) error { return r.CommentHotspot(cmd.Context(), key, res.Comment) }
			default:
				resolution, ok := approval.HotspotResolutions[verb]
				if !ok {
					return usageErr("unknown action %q (use %s)", args[1], hotspotVerbs())
				}
				res = ChangeResult{Action: verb, APICall: "POST /api/hotspots/change_status status=REVIEWED resolution=" + resolution}
				do = func(r *sonarcli.Runner) error { return r.ReviewHotspot(cmd.Context(), key, resolution, res.Comment) }
			}
			if err := approval.Check(c.approved, c.comment); err != nil {
				return err
			}
			res.Key, res.Kind, res.Comment, res.DryRun = key, "hotspot", approval.ServerComment(c.comment), c.dryRun
			return a.applyChange(cmd, &res, func() error {
				r, err := a.runner()
				if err != nil {
					return err
				}
				return do(r)
			})
		},
	}
	c.register(cmd)
	return cmd
}

func (a *app) applyChange(_ *cobra.Command, res *ChangeResult, do func() error) error {
	entry := approval.NewEntry(res.Kind, res.Key, res.Action, res.Comment)
	entry.DryRun = res.DryRun
	var opErr error
	if res.DryRun {
		res.Result = "dry-run"
	} else if opErr = do(); opErr != nil {
		res.Result = "error"
		entry.ErrorMsg = opErr.Error()
	} else {
		res.Result = "ok"
	}
	entry.Result = res.Result
	p, err := a.audit(entry)
	if err != nil {
		_, _ = fmt.Fprintf(a.stderr, "sqa: warning: could not write audit log: %v\n", err)
	} else {
		res.AuditFile = p
	}
	if opErr != nil {
		return opErr
	}
	return a.out(*res)
}
