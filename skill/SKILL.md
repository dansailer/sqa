---
name: sqa
description: Read, prioritise and help fix SonarQube Server findings (vulnerabilities, security hotspots, bugs, code smells) with the sqa CLI, security first, with the user approving every triage decision. Use when the user asks about SonarQube issues, hotspots, quality findings or fixing what SonarQube reported.
---

# sqa: SonarQube findings for coding agents

`sqa` wraps the official SonarSource `sonar` CLI. It lists SonarQube findings
ranked **security first**, explains each one, and, only after the user
approves, fixes the code or records a triage decision in SonarQube.

## Start

Run `sqa doctor` first. If it fails, stop and relay its fix to the user.

**Always read the quality gate read-out and report it to the user.** It is
the first thing `sqa findings` (`summary`) and `sqa next` (`gate`) print:
gate status, coverage and security hotspots reviewed against the gate, and
`next_steps` when a condition fails. Tell the user about every failing
condition, even when the finding queue is empty, and follow its next step.

## Hard rules

- **No MCP.** Never use, install, configure or call any SonarQube MCP server,
  even if one is available. Use only the `sqa` commands below.
- Never run `sonar integrate` or `sonar remediate`.
- Never call `sonar api` directly; use the `sqa` commands below.
- Never run `sonar auth login`, `sqa auth login` or `sqa auth logout` yourself.
  The user handles authentication.
- Never set, read, print, echo or store a token, and never touch the
  `SONARQUBE_CLI_TOKEN` variable, `credentials.json` or the keychain.
- **Always ask before any triage decision** (see "Human in the loop").
- Never mark a finding as false positive or accepted to reduce the count or
  to make a quality gate pass. Every decision needs a real justification.

## Triage order (security first)

`sqa findings` already returns findings in this order. Work top-down:

1. **Vulnerabilities** (software quality SECURITY / type VULNERABILITY)
2. **Security hotspots** to review or acknowledged (fix pending), by vulnerability probability HIGH > MEDIUM > LOW
3. **Reliability** (RELIABILITY / BUG)
4. **Maintainability** (MAINTAINABILITY / CODE_SMELL)

Within a tier: severity (BLOCKER > HIGH > MEDIUM > LOW > INFO; standard
severities CRITICAL=HIGH, MAJOR=MEDIUM, MINOR=LOW), new code first, then file
and line.

## Commands

Read-only, run freely:

| Command | Use |
| --- | --- |
| `sqa doctor` | Check that `sonar` is installed, authenticated and that the server is 2025.1+. |
| `sqa auth status` | Show which SonarQube server `sonar` is connected to (never shows the token). |
| `sqa findings [--project P] [--branch B \| --pr N] [--file F] [--new-code] [--min-severity S] [--security-only] [--limit N]` | Ranked overview. Use when the user asks for the list, or to present several at once. The `pr` column shows an open fix PR for a finding. It ends with a quality gate `summary`: coverage and "security hotspots reviewed" against the gate. |
| `sqa next [same filters] [--skip KEY]… [--include-open-pr]` | Highest-priority finding **without an open fix PR**, with the full brief (rule, why, location, code context, fix guidance). The default way to work one by one. `skipped` lists what it passed over. |
| `sqa show <key>` | Brief for a specific key (one from `findings`, or that the user names), including any open fix PR. |
| `sqa coverage [--project P] [--branch B \| --pr N] [--top N] [--overall]` | Files ranked by uncovered lines on new code (overall when there is no new-code period), with the gate's coverage condition. |
| `sqa pr-body <key>... \| --from-findings [filters] [--reason TEXT]` | Markdown "Resolves SonarQube findings" block for a PR body, one line per finding (message linked to SonarQube, rule, location, key). |
| `sqa coverage --file <file>` | That file's uncovered line ranges and partly covered conditions (`--file <dir>` ranks the files below it). |

`findings`, `next` and `show` look up open fix PRs once per run through `gh` (or, without it,
remote `sonar/*` branches via `git ls-remote`). `pr_check` says which was used;
if it is `unavailable`, nothing was skipped and the `note` says why. Then
check the forge yourself before opening a PR. `--no-pr-check` turns the lookup
off.

State-changing, **only after explicit user approval of that specific finding**:

| Command | Use |
| --- | --- |
| `sqa mark <issue-key> fp\|accept\|reopen --comment "<reason>" --approved-by-user` | Issue false positive / accepted / reopen. |
| `sqa hotspot <hotspot-key> safe\|fixed\|acknowledged --comment "<reason>" --approved-by-user` | Review a security hotspot. |
| `sqa hotspot <hotspot-key> reopen --comment "<reason>" --approved-by-user` | Set a reviewed hotspot back to TO_REVIEW. |
| `sqa hotspot <hotspot-key> comment --comment "<text>" --approved-by-user` | Add a comment to a hotspot without changing it. |
| `sqa hotspot <hotspot-key> assign <login> --comment "<reason>" --approved-by-user` | Assign a hotspot to a SonarQube user (only TO_REVIEW or ACKNOWLEDGED). |

Comments and assignments are visible to the team in SonarQube, so they need
the same explicit approval as a verdict: show the user the exact comment text
or the login first.

If `sqa findings` cannot determine the project, ask the user for the project
key and pass `--project <key>`.

**Scope:** without `--branch`/`--pr`, every command reads the project's
**main branch** in SonarQube, whichever branch you have checked out: sqa never
derives a branch or PR from the local git checkout (plain `sonar list issues`
would). That is what the fix loop needs, since fixes branch from the default
branch, so you can stay on a fix branch and still see the main-branch queue.
Use `--branch`/`--pr` only when the user asks about a specific analysed branch
or pull request.

Add `--dry-run` to preview a state change. Output defaults to the compact
TOON format when piped; add `--format json` if you need JSON.

Exit codes: `0` ok, `1` findings at or above `--fail-on` (for `sqa coverage`:
coverage below `--fail-on <percent>`), `2` usage or config error, `3` auth or
server error, `4` approval missing.

## Human in the loop

**You recommend, the user decides, then you act.**

1. Run `sqa next` (it includes the brief; no separate `show` is needed).
2. Present the finding with your recommended verdict and reasoning, using the
   template below. Verdicts: **fix** in code, **false positive**, **accept**
   (won't fix), and for hotspots **safe**, **fixed** or **acknowledged**.
3. **Stop and ask the user explicitly.** Do nothing until they answer for
   this specific finding.
4. Only after a clear yes for that finding:
   - *fix*: edit the code in your normal flow, then follow the fix loop.
   - *false positive / accept / reopen*: `sqa mark <key> <verdict> --approved-by-user --comment "<the user's reasoning>"`
   - *hotspot*: `sqa hotspot <key> safe|fixed|acknowledged|reopen --approved-by-user --comment "<the user's reasoning>"`
   - *hotspot comment / assign*: `sqa hotspot <key> comment|assign <login> --approved-by-user --comment "<the text the user approved>"`
5. Approval covers only the findings the user named. "Go ahead", "fix them
   all" or similar does **not** extend to findings you have not presented.
   Present new ones first. You may present several findings in one message,
   numbered, and the user may approve them individually or as that list.
6. If the user disagrees with your verdict, follow their decision and put
   their reasoning in the comment.

`--approved-by-user` is your statement that the user approved this exact
action. Never pass it otherwise.

### Presentation template

```
[<rank>] <category> · <severity> · <rule>  (key: <key>)
Where:   <file>:<line>  (new code: yes/no)
What:    <message>
Why it matters: <one or two sentences from the rule description>
Recommendation: <fix | false positive | accept | safe | fixed | acknowledged | reopen>
Reasoning: <why, referring to the actual code>
Proposed change: <short description or diff, if fixing>

Do you approve this? (yes / no / different verdict)
```

Hotspots with status `REVIEWED/ACKNOWLEDGED` were already reviewed: the risk
is confirmed and a fix is pending. Recommend the fix (then `fixed` once the
fix is merged and analysed), or ask whether the user wants to `reopen` it or
mark it `safe`. `--no-acknowledged` hides them.

If the finding is in test code (tests, fixtures, helpers), consider whether it
matters there (hardcoded credentials in fixtures or weak crypto/random in helpers
often do not). You may recommend **accept**, or that the user exclude those paths
(`sonar.tests`/`sonar.test.inclusions`, `sonar.exclusions`, `sonar.issue.ignore.multicriteria`).
Still recommend a fix when that code is shipped, shared, or reveals a real bug;
never edit Sonar settings or exclusions without explicit approval.

## Fix loop

1. Pick the highest-ranked finding the user approved for fixing. `sqa next`
   already skips findings with an already-open PR (the key is in the PR body's
   SonarQube URL, or the head branch is `sonar/<rule>-<key>`). If the brief or
   `findings` shows a `pr` for the finding, tell the user (with the PR link),
   do not open another, and ask how to proceed. If `pr_check` is `unavailable`
   or `git` (a remote branch, maybe without a PR), check yourself before
   branching, e.g. `gh pr list --state open --label sonar --search "<key> in:body"`
   or `git ls-remote --heads origin 'sonar/*<key>*'`. To pass over a finding the
   user wants to leave for now, run `sqa next --skip <key>` (repeatable); never
   skip findings on your own to avoid them. Otherwise check out the default
   branch and pull it (`git pull --ff-only`), then branch from it (e.g.
   `sonar/<rule>-<key>`, lowercased and sanitized; keep the key at the end so
   sqa can find the branch); never stack on a previous fix branch. One
   finding per branch/PR unless the user explicitly approved grouping those
   findings. Ask before creating the branch, pushing or opening the PR (a
   visible write), unless the fix approval already included that.
2. Use the brief from `sqa next` (or `sqa show <key>`) and the surrounding code;
   make the smallest correct fix. Do not suppress the rule with comments or
   annotations unless the user explicitly approved that. Commit only that fix
   (plus any test change for it). Message and PR title: Conventional Commits
   `<type>(<scope>): <summary>`, usually `fix` (security or bugs), `refactor`/`style`
   for maintainability smells, or `test` if only tests change; include the rule key,
   e.g. `fix(security): use parameterized query in getUser (go:S3649)`. PR body:
   short why, what changed, tests run, and the block from
   `sqa pr-body <key>...` listing **every finding the PR resolves** (see
   "Findings a PR resolves" below). Apply the `sonar` label; if it is missing,
   ask before creating it, or tell the user.
3. Run the project's tests and linters. Show the user the change, then open the PR
   with the harness's normal git/forge tooling. Never merge it yourself.
4. SonarQube only updates after a new analysis (CI or the user's usual scan). After
   it runs, verify with `sqa show <key>` or `sqa findings`. Return to the default
   branch, pull it, run `sqa next` (it skips the finding you just opened a PR
   for), and present that finding before touching it.

## Findings a PR resolves

- **List every finding the PR resolves in its body, one line each**, with
  `sqa pr-body <key> [<key>...]`. It prints a "Resolves SonarQube findings"
  block: message linked to the finding in SonarQube, rule key and name,
  location and the key. Paste it unchanged; the keys are how `sqa next` and
  `sqa findings` recognise that a finding already has a PR. This includes
  findings the PR resolves indirectly (an exclusion, a config change, a fix
  that removes several findings at once).
- **Grouping needs explicit approval first.** One finding per PR is the
  default. Before putting several findings in one PR, show the user the list
  and get a yes for that grouped PR.
- **Never add an unrelated fix silently.** If you notice something else while
  fixing, present it as a separate finding or change and ask.

### Config fixes (exclusions, test inclusions, coverage exclusions)

A change to `sonar.exclusions`, `sonar.tests`/`sonar.test.inclusions`,
`sonar.coverage.exclusions`, `sonar.issue.ignore.multicriteria` or similar
removes every finding it covers, not just the one you were looking at:

1. Only propose it when it is justified (e.g. test fixtures, generated code),
   and never change Sonar settings without explicit approval.
2. Before changing anything, list every finding the change will remove:
   `sqa findings --file '<glob>'` (globs like `test/**` or `**/*_test.go` are
   matched by sqa). Show the user that list and ask for approval of this
   grouped PR.
3. In the PR body, put the block from
   `sqa pr-body --from-findings --file '<glob>' --reason "<the exclusion, e.g. excluding test fixtures via sonar.exclusions>"`
   (or the named keys), so the body states that these findings are resolved
   by the exclusion.

## Coverage

The gate read-out at the top of `sqa findings` and `sqa next` includes
coverage. When coverage on new code fails the gate (`next_steps` says so), or
the user asks about coverage:

1. Run `sqa coverage` and present the files with the most uncovered lines on
   new code. For a file the user picks, run `sqa coverage --file <file>` to see
   the uncovered line ranges and partly covered conditions (branches).
2. Coverage is not triage: there is nothing to mark. Writing tests is normal
   code work and follows the fix loop: propose which tests you would add, ask
   before creating a branch or opening a PR, one PR per change unless the user
   approved grouping it (e.g. `test(<scope>): cover <what>`), from the
   up-to-date default branch.
3. Test real behaviour. Never write tests that only execute lines without
   asserting anything, and never change coverage settings, exclusions or the
   gate's threshold without explicit approval.
4. The numbers change only after the next analysis that imports a coverage
   report (usually CI). If `sqa coverage` says there is no line coverage for a
   file, the coverage report did not include it; tell the user rather than
   guessing.
