package cli

import (
	"context"

	"github.com/dansailer/sqa/internal/sonarcli"
)

// scope is the project and the branch or pull request a command reads.
// Without --branch and --pr it is the project's main branch: sqa never
// derives a branch or PR from the local git checkout.
type scope struct {
	project, branch, pr string
}

const noProjectHint = "could not determine the project key: pass --project, set default_project in config.yaml, or run `sonar link <projectKey>` in the repository"

// resolveScope takes the project from --project, then default_project, then
// sonar's auto-detection (sonar link / git remote), which costs one
// `sonar list issues --page-size 1` call. Branch and PR are the flags only.
func (a *app) resolveScope(ctx context.Context, r *sonarcli.Runner, project, branch, pr string) (scope, error) {
	if branch != "" && pr != "" {
		return scope{}, usageErr("--branch and --pr cannot be combined")
	}
	if project == "" {
		project = a.cfg.DefaultProject
	}
	sc := scope{project: project, branch: branch, pr: pr}
	if sc.project != "" {
		return sc, nil
	}
	_, _, det, err := r.ListIssues(ctx, sonarcli.ListIssuesOptions{Branch: branch, PullRequest: pr, PageSize: 1}, 1)
	if err != nil {
		if isNoProject(err) {
			return sc, usageErr(noProjectHint)
		}
		return sc, err
	}
	if sc.project = det.Project; sc.project == "" {
		return sc, usageErr(noProjectHint)
	}
	return sc, nil
}

// listBranch is the branch to pass to `sonar list issues`. Without --branch
// or --pr that is the main branch by name, because sonar would otherwise pick
// a pull request from the local git branch.
func (sc scope) listBranch(ctx context.Context, r *sonarcli.Runner) (string, error) {
	if sc.branch != "" || sc.pr != "" {
		return sc.branch, nil
	}
	return r.MainBranch(ctx, sc.project)
}
