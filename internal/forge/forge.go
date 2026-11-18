// Package forge finds open fix pull requests for SonarQube findings on the
// code forge, so `sqa next` can skip findings that are already being fixed.
//
// It never talks to SonarQube and never handles tokens: it runs `gh`, which
// uses its own login, and falls back to `git ls-remote`, which uses the
// repository's normal git credentials. One listing call per invocation.
package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Source names the tool that produced an Index.
const (
	SourceGH  = "gh"
	SourceGit = "git"
)

// timeout bounds each forge call so a slow or prompting forge never stalls sqa.
const timeout = 15 * time.Second

// ghLimit is how many open PRs are listed in one call.
const ghLimit = 200

// PR is an open pull request (gh) or a remote fix branch (git fallback).
type PR struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch"`
	body   string
}

// Ref is the short form shown in listings: the PR URL, or `branch:<name>`
// when only a remote branch is known.
func (p PR) Ref() string {
	if p.URL != "" {
		return p.URL
	}
	return "branch:" + p.Branch
}

// Index holds the open PRs/branches found in one listing call.
type Index struct {
	Source string // SourceGH, SourceGit or "" when detection was unavailable
	Note   string // why detection degraded, empty when gh worked
	prs    []PR
}

// Available reports whether any forge listing worked.
func (ix Index) Available() bool { return ix.Source != "" }

// Lookup returns the open PR (or fix branch) for a finding key: a PR whose
// body contains the key as a whole token (the SonarQube link), or whose head
// branch ends with it (`sonar/<rule>-<key>`, lowercased and sanitized).
func (ix Index) Lookup(key string) (PR, bool) {
	nk := sanitize(key)
	if nk == "" {
		return PR{}, false
	}
	for _, p := range ix.prs {
		if containsToken(p.body, key) || branchHasKey(p.Branch, nk) {
			return p, true
		}
	}
	return PR{}, false
}

// keyChar reports whether r can be part of a SonarQube issue/hotspot key.
func keyChar(r byte) bool {
	return r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// containsToken reports whether key occurs in s not directly next to another
// key character, so AX-1 does not match AX-12.
func containsToken(s, key string) bool {
	for i := 0; key != ""; {
		j := strings.Index(s[i:], key)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(key)
		if (start == 0 || !keyChar(s[start-1])) && (end == len(s) || !keyChar(s[end])) {
			return true
		}
		i = start + 1
	}
	return false
}

// sanitize lowercases s and turns every run of characters outside [a-z0-9]
// into one '-', trimming them at the ends; branch names get the same
// treatment, so the separator a harness chose does not matter.
func sanitize(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

// branchHasKey reports whether the sanitized branch ends with the sanitized
// key on a separator boundary.
func branchHasKey(branch, nk string) bool {
	nb := sanitize(branch)
	return nb == nk || strings.HasSuffix(nb, "-"+nk)
}

// Runner runs a command in dir and returns its stdout. Replaced in tests.
type Runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH", name)
	}
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // fixed forge tools, fixed arguments
	cmd.Dir = dir
	// Never let gh or git prompt (they would hang an agent).
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0", "GH_NO_UPDATE_NOTIFIER=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg != "" {
			return nil, fmt.Errorf("%s: %s", name, msg)
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Detect lists open PRs in the repository at dir ("" = current directory):
// `gh pr list` first, then `git ls-remote --heads origin 'sonar/*'`. It never
// fails; when neither works the Index is empty and Note says why.
func Detect(ctx context.Context, dir string) Index {
	return detect(ctx, dir, execRunner)
}

func detect(ctx context.Context, dir string, run Runner) Index {
	ix, ghErr := listGH(ctx, dir, run)
	if ghErr == nil {
		return ix
	}
	ix, gitErr := listGit(ctx, dir, run)
	if gitErr != nil {
		return Index{Note: fmt.Sprintf("open-PR check unavailable (%v; %v)", ghErr, gitErr)}
	}
	ix.Note = fmt.Sprintf("open-PR check used remote sonar/* branches (%v); a branch may have no open PR", ghErr)
	return ix
}

func listGH(ctx context.Context, dir string, run Runner) (Index, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(c, dir, "gh", "pr", "list", "--state", "open", "--limit", strconv.Itoa(ghLimit),
		"--json", "number,url,headRefName,body")
	if err != nil {
		return Index{}, err
	}
	var raw []struct {
		Number      int    `json:"number"`
		URL         string `json:"url"`
		HeadRefName string `json:"headRefName"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Index{}, fmt.Errorf("gh: unexpected output: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		prs = append(prs, PR{Number: p.Number, URL: p.URL, Branch: p.HeadRefName, body: p.Body})
	}
	return Index{Source: SourceGH, prs: prs}, nil
}

func listGit(ctx context.Context, dir string, run Runner) (Index, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(c, dir, "git", "ls-remote", "--heads", "origin", "refs/heads/sonar/*")
	if err != nil {
		return Index{}, err
	}
	var prs []PR
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasPrefix(f[1], "refs/heads/") {
			prs = append(prs, PR{Branch: strings.TrimPrefix(f[1], "refs/heads/")})
		}
	}
	return Index{Source: SourceGit, prs: prs}, nil
}
