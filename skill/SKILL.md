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
2. **Security hotspots to review**, by vulnerability probability HIGH > MEDIUM > LOW
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
| `sqa findings [--project P] [--branch B \| --pr N] [--file F] [--new-code] [--min-severity S] [--security-only] [--limit N]` | Ranked overview. Use when the user asks for the list, or to present several at once. |
| `sqa next [same filters]` | Highest-priority finding with the full brief (rule, why, location, code context, fix guidance). The default way to work one by one. |
| `sqa show <key>` | Brief for a specific key (one from `findings`, or that the user names). |

State-changing, **only after explicit user approval of that specific finding**:

| Command | Use |
| --- | --- |
| `sqa mark <issue-key> fp\|accept\|reopen --comment "<reason>" --approved-by-user` | Issue false positive / accepted / reopen. |
| `sqa hotspot <hotspot-key> safe\|fixed\|acknowledged --comment "<reason>" --approved-by-user` | Review a security hotspot. |

If `sqa findings` cannot determine the project, ask the user for the project
key and pass `--project <key>`.

Add `--dry-run` to preview a state change. Output defaults to the compact
TOON format when piped; add `--format json` if you need JSON.

Exit codes: `0` ok, `1` findings at or above `--fail-on`, `2` usage or config
error, `3` auth or server error, `4` approval missing.

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
   - *hotspot*: `sqa hotspot <key> safe|fixed|acknowledged --approved-by-user --comment "<the user's reasoning>"`
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
Recommendation: <fix | false positive | accept | safe | fixed | acknowledged>
Reasoning: <why, referring to the actual code>
Proposed change: <short description or diff, if fixing>

Do you approve this? (yes / no / different verdict)
```

If the finding is in test code (tests, fixtures, helpers), consider whether it
matters there (hardcoded credentials in fixtures or weak crypto/random in helpers
often do not). You may recommend **accept**, or that the user exclude those paths
(`sonar.tests`/`sonar.test.inclusions`, `sonar.exclusions`, `sonar.issue.ignore.multicriteria`).
Still recommend a fix when that code is shipped, shared, or reveals a real bug;
never edit Sonar settings or exclusions without explicit approval.

## Fix loop

1. Pick the highest-ranked finding the user approved for fixing. Before
   creating a branch, check the forge for an already-open PR/MR on that
   finding: the key is in the body's SonarQube URL, the PR has the `sonar`
   label, and the head is typically `sonar/<rule>-<key>`. Example:
   `gh pr list --state open --label sonar --search "<key> in:body"`. If `gh`
   is not available: `git ls-remote --heads origin 'sonar/*<key>*'`. If one
   exists, tell the user (with the PR link), do not open another, and ask
   how to proceed. `sqa` has no skip or defer command; only run `sqa next`
   after the user agrees. Otherwise check out the default branch and pull
   it (`git pull --ff-only`), then branch from it (e.g. `sonar/<rule>-<key>`,
   lowercased and sanitized); never stack on a previous fix branch. One
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
   `[<message> (<rule> · <rule name>)](<SonarQube URL from sqa next/show>)`,
   location, short why, what changed, tests run — not the raw finding key.
   Apply the `sonar` label; if it is missing, ask before creating it, or tell
   the user.
3. Run the project's tests and linters. Show the user the change, then open the PR
   with the harness's normal git/forge tooling. Never merge it yourself.
4. SonarQube only updates after a new analysis (CI or the user's usual scan). After
   it runs, verify with `sqa show <key>` or `sqa findings`. Return to the default
   branch, pull it, run `sqa next`, and present that finding before touching it.
