// Package approval enforces the human-in-the-loop guardrails for commands
// that change state in SonarQube and records an audit trail.
package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// ErrMissing is returned when a state change lacks explicit user approval or
// a justification comment.
var ErrMissing = errors.New("approval missing")

// CommentPrefix marks comments written by sqa on the SonarQube server.
const CommentPrefix = "[sqa] "

// Check validates the guardrails: --approved-by-user must be set and the
// comment must be non-empty.
func Check(approvedByUser bool, comment string) error {
	var missing []string
	if !approvedByUser {
		missing = append(missing, "--approved-by-user (the user must explicitly approve this specific finding first)")
	}
	if strings.TrimSpace(comment) == "" {
		missing = append(missing, `--comment "<the user's reasoning>"`)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: required %s", ErrMissing, strings.Join(missing, " and "))
	}
	return nil
}

// ServerComment returns the comment as written to SonarQube.
func ServerComment(comment string) string {
	c := strings.TrimSpace(comment)
	if strings.HasPrefix(c, strings.TrimSpace(CommentPrefix)) {
		return c
	}
	return CommentPrefix + c
}

// IssueTransitions maps sqa verbs to /api/issues/do_transition transitions.
var IssueTransitions = map[string]string{
	"fp":     "falsepositive",
	"accept": "accept",
	"reopen": "reopen",
}

// HotspotResolutions maps sqa verbs to /api/hotspots/change_status
// resolutions (status REVIEWED).
var HotspotResolutions = map[string]string{
	"safe":         "SAFE",
	"fixed":        "FIXED",
	"acknowledged": "ACKNOWLEDGED",
}

// Entry is one line of the audit log.
type Entry struct {
	Time     string `json:"time"`
	OSUser   string `json:"os_user"`
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Action   string `json:"action"`
	Server   string `json:"server,omitempty"`
	Comment  string `json:"comment"`
	DryRun   bool   `json:"dry_run"`
	Result   string `json:"result"`
	ErrorMsg string `json:"error,omitempty"`
}

// NewEntry fills time and OS user.
func NewEntry(kind, key, action, comment string) Entry {
	e := Entry{Time: time.Now().UTC().Format(time.RFC3339), Kind: kind, Key: key, Action: action, Comment: comment}
	if u, err := user.Current(); err == nil {
		e.OSUser = u.Username
	}
	return e
}

// Append writes e as one JSON line to path (created 0600, parent 0700).
func Append(path string, e Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // audit log path
	if err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
