package sonarcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Version returns the sonar CLI version (e.g. "1.12.0").
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, _, err := r.Run(ctx, "--version")
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimPrefix(strings.TrimSpace(lines[len(lines)-1]), "v"), nil
}

// AuthStatus is the JSON printed by `sonar auth status --format json`.
type AuthStatus struct {
	Status  string `json:"status"`
	Server  string `json:"server,omitempty"`
	Org     string `json:"org,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// Connected reports whether sonar has a working connection.
func (a AuthStatus) Connected() bool { return a.Status == "connected" }

// AuthStatus runs `sonar auth status --format json`. sonar exits 1 for any
// non-connected state but still prints the JSON object, so a parseable body
// is returned without error.
func (r *Runner) AuthStatus(ctx context.Context) (AuthStatus, error) {
	out, _, err := r.Run(ctx, "auth", "status", "--format", "json")
	var st AuthStatus
	if jerr := json.Unmarshal(out, &st); jerr == nil && st.Status != "" {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, fmt.Errorf("unexpected output from sonar auth status: %q", truncate(string(out), 200))
}

// apiGet runs `sonar api get <path>` and returns the response body.
func (r *Runner) apiGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	out, _, err := r.Run(ctx, "api", "get", withQuery(path, query))
	return out, err
}

// getJSON runs `sonar api get` and decodes the JSON body into v.
func (r *Runner) getJSON(ctx context.Context, path string, q url.Values, v any) error {
	out, err := r.apiGet(ctx, path, q)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// post runs `sonar api post <path> --data <json>` and drops the body. sonar
// sends the JSON object as form data for /api/ (v1) endpoints.
func (r *Runner) post(ctx context.Context, path string, data map[string]string) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, _, err = r.Run(ctx, "api", "post", path, "--data", string(b))
	return err
}

func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// ListIssuesOptions maps to `sonar list issues` flags.
type ListIssuesOptions struct {
	Project     string
	Branch      string
	PullRequest string
	File        string
	NewCode     bool
	Statuses    []string
	Severities  []string
	PageSize    int
	Page        int
}

// Args renders the options as sonar arguments.
func (o ListIssuesOptions) Args() []string {
	args := []string{"list", "issues", "--format", "json"}
	add := func(flag, v string) {
		if v != "" {
			args = append(args, flag, v)
		}
	}
	add("--project", o.Project)
	add("--branch", o.Branch)
	add("--pull-request", o.PullRequest)
	add("--file", o.File)
	add("--statuses", strings.Join(o.Statuses, ","))
	add("--severities", strings.Join(o.Severities, ","))
	if o.NewCode {
		args = append(args, "--new-code")
	}
	if o.PageSize > 0 {
		args = append(args, "--page-size", strconv.Itoa(o.PageSize))
	}
	if o.Page > 0 {
		args = append(args, "--page", strconv.Itoa(o.Page))
	}
	return args
}

// Detected holds what sonar auto-detected and announced on stderr. sqa only
// takes the project key from it: sonar also picks a pull request from the
// local git branch when neither --branch nor --pull-request is given, which
// sqa avoids by always passing one (see MainBranch).
type Detected struct {
	Project string
}

var reProject = regexp.MustCompile(`Using auto-detected project key:\s*(\S+)`)

// ParseDetected extracts the auto-detected project key from sonar stderr.
func ParseDetected(stderr []byte) Detected {
	var d Detected
	if m := reProject.FindSubmatch(stderr); m != nil {
		d.Project = strings.Trim(string(m[1]), "'\"`.")
	}
	return d
}

// MainBranch returns the name of the project's main branch
// (/api/project_branches/list).
func (r *Runner) MainBranch(ctx context.Context, project string) (string, error) {
	q := url.Values{}
	q.Set("project", project)
	var resp struct {
		Branches []struct {
			Name   string `json:"name"`
			IsMain bool   `json:"isMain"`
		} `json:"branches"`
	}
	if err := r.getJSON(ctx, "/api/project_branches/list", q, &resp); err != nil {
		return "", err
	}
	for _, b := range resp.Branches {
		if b.IsMain && b.Name != "" {
			return b.Name, nil
		}
	}
	return "", fmt.Errorf("project %s has no main branch in /api/project_branches/list", project)
}

// maxIssuePages caps pagination (SonarQube refuses results past 10 000).
const maxIssuePages = 20

// ListIssues runs `sonar list issues --format json`, following pagination
// until all results (or limit, when > 0) are collected.
func (r *Runner) ListIssues(ctx context.Context, o ListIssuesOptions, limit int) ([]Issue, []Component, Detected, error) {
	var (
		issues []Issue
		comps  []Component
		det    Detected
	)
	if o.PageSize == 0 {
		o.PageSize = 500
	}
	for page := 1; page <= maxIssuePages; page++ {
		o.Page = page
		out, stderr, err := r.Run(ctx, o.Args()...)
		if err != nil {
			return nil, nil, det, err
		}
		if page == 1 {
			det = ParseDetected(stderr)
			if o.Project == "" && det.Project != "" {
				o.Project = det.Project
			}
		}
		var resp IssuesSearchResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, nil, det, fmt.Errorf("parse sonar list issues output: %w", err)
		}
		issues = append(issues, resp.Issues...)
		comps = append(comps, resp.Components...)
		total := resp.Total
		if resp.Paging.Total > 0 {
			total = resp.Paging.Total
		}
		if len(resp.Issues) == 0 || len(issues) >= total || (limit > 0 && len(issues) >= limit) {
			break
		}
	}
	return issues, comps, det, nil
}

// SearchHotspotsOptions maps to /api/hotspots/search parameters.
type SearchHotspotsOptions struct {
	Project     string
	Branch      string
	PullRequest string
	Files       []string
	NewCode     bool
	Status      string
	Resolution  string
}

// SearchHotspots calls /api/hotspots/search through `sonar api get`, following
// pagination.
func (r *Runner) SearchHotspots(ctx context.Context, o SearchHotspotsOptions, limit int) ([]Hotspot, []Component, error) {
	var (
		hs    []Hotspot
		comps []Component
	)
	for page := 1; page <= maxIssuePages; page++ {
		q := url.Values{}
		q.Set("project", o.Project)
		setIf(q, "branch", o.Branch)
		setIf(q, "pullRequest", o.PullRequest)
		setIf(q, "status", o.Status)
		setIf(q, "resolution", o.Resolution)
		if len(o.Files) > 0 {
			q.Set("files", strings.Join(o.Files, ","))
		}
		if o.NewCode {
			q.Set("inNewCodePeriod", "true")
		}
		q.Set("ps", "500")
		q.Set("p", strconv.Itoa(page))
		var resp HotspotsSearchResponse
		if err := r.getJSON(ctx, "/api/hotspots/search", q, &resp); err != nil {
			return nil, nil, err
		}
		hs = append(hs, resp.Hotspots...)
		comps = append(comps, resp.Components...)
		if len(resp.Hotspots) == 0 || len(hs) >= resp.Paging.Total || (limit > 0 && len(hs) >= limit) {
			break
		}
	}
	return hs, comps, nil
}

// GetIssue fetches a single issue by key via /api/issues/search?issues=KEY,
// on the given branch or pull request (older servers only search the main
// branch without them). It returns (nil, nil) when no issue has that key.
func (r *Runner) GetIssue(ctx context.Context, key, branch, pr string) (*Issue, []Component, error) {
	q := url.Values{}
	q.Set("issues", key)
	q.Set("additionalFields", "_all")
	setIf(q, "branch", branch)
	setIf(q, "pullRequest", pr)
	var resp IssuesSearchResponse
	if err := r.getJSON(ctx, "/api/issues/search", q, &resp); err != nil {
		return nil, nil, err
	}
	for i := range resp.Issues {
		if resp.Issues[i].Key == key {
			return &resp.Issues[i], resp.Components, nil
		}
	}
	return nil, nil, nil
}

// ShowHotspot calls /api/hotspots/show. A 404 from the server surfaces as a
// sonar error.
func (r *Runner) ShowHotspot(ctx context.Context, key string) (*HotspotDetail, error) {
	q := url.Values{}
	q.Set("hotspot", key)
	var h HotspotDetail
	if err := r.getJSON(ctx, "/api/hotspots/show", q, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ShowRule calls /api/rules/show.
func (r *Runner) ShowRule(ctx context.Context, key string) (*Rule, error) {
	q := url.Values{}
	q.Set("key", key)
	var resp struct {
		Rule Rule `json:"rule"`
	}
	if err := r.getJSON(ctx, "/api/rules/show", q, &resp); err != nil {
		return nil, err
	}
	return &resp.Rule, nil
}

// SourceLines calls /api/sources/lines for [from, to] (the whole file when
// from and to are 0).
func (r *Runner) SourceLines(ctx context.Context, component string, from, to int, branch, pr string) ([]SourceLine, error) {
	q := url.Values{}
	q.Set("key", component)
	if from > 0 {
		q.Set("from", strconv.Itoa(from))
	}
	if to > 0 {
		q.Set("to", strconv.Itoa(to))
	}
	setIf(q, "branch", branch)
	setIf(q, "pullRequest", pr)
	var resp struct {
		Sources []SourceLine `json:"sources"`
	}
	if err := r.getJSON(ctx, "/api/sources/lines", q, &resp); err != nil {
		return nil, err
	}
	sort.Slice(resp.Sources, func(i, j int) bool { return resp.Sources[i].Line < resp.Sources[j].Line })
	return resp.Sources, nil
}

// ServerVersion calls /api/server/version (plain-text body).
func (r *Runner) ServerVersion(ctx context.Context) (string, error) {
	out, err := r.apiGet(ctx, "/api/server/version", nil)
	if err != nil {
		return "", err
	}
	return strings.Trim(strings.TrimSpace(string(out)), `"`), nil
}

// TransitionIssue calls /api/issues/do_transition.
func (r *Runner) TransitionIssue(ctx context.Context, key, transition string) error {
	return r.post(ctx, "/api/issues/do_transition", map[string]string{"issue": key, "transition": transition})
}

// CommentIssue calls /api/issues/add_comment.
func (r *Runner) CommentIssue(ctx context.Context, key, text string) error {
	return r.post(ctx, "/api/issues/add_comment", map[string]string{"issue": key, "text": text})
}

// ReviewHotspot calls /api/hotspots/change_status with status REVIEWED.
func (r *Runner) ReviewHotspot(ctx context.Context, key, resolution, comment string) error {
	return r.post(ctx, "/api/hotspots/change_status", map[string]string{
		"hotspot": key, "status": "REVIEWED", "resolution": resolution, "comment": comment,
	})
}

func setIf(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ReopenHotspot calls /api/hotspots/change_status with status TO_REVIEW (a
// resolution is not allowed then).
func (r *Runner) ReopenHotspot(ctx context.Context, key, comment string) error {
	return r.post(ctx, "/api/hotspots/change_status", map[string]string{
		"hotspot": key, "status": "TO_REVIEW", "comment": comment,
	})
}

// CommentHotspot calls /api/hotspots/add_comment (an internal endpoint the
// SonarQube UI uses, available since 8.1).
func (r *Runner) CommentHotspot(ctx context.Context, key, comment string) error {
	return r.post(ctx, "/api/hotspots/add_comment", map[string]string{"hotspot": key, "comment": comment})
}

// AssignHotspot calls /api/hotspots/assign (an internal endpoint the
// SonarQube UI uses, available since 8.2). The server only allows it on
// TO_REVIEW or ACKNOWLEDGED hotspots.
func (r *Runner) AssignHotspot(ctx context.Context, key, assignee, comment string) error {
	return r.post(ctx, "/api/hotspots/assign", map[string]string{"hotspot": key, "assignee": assignee, "comment": comment})
}
