# sqa: Design

Status: Implemented in v0.1.0 · Date: 2026-11-17 · Owner: Dan Sailer

## 1. Goal

A single-binary Go CLI (`sqa`) that lets coding harnesses (Grok, OpenCode, Claude Code, Cursor, …) read findings from internally hosted SonarQube Server, prioritised **security first**, and help fix them — with the **user always in control of triage decisions**. The CLI ships its own harness-neutral `SKILL.md` that any harness can install.

## 2. Constraints

| # | Constraint |
|---|------------|
| C1 | Targets internally hosted **SonarQube Server 2025.1+**. |
| C2 | **Contains / builds on the official SonarSource `sonar` CLI** ([SonarSource/sonarqube-cli](https://github.com/SonarSource/sonarqube-cli)). |
| C3 | **No MCP.** Must not install, configure or call any SonarQube MCP server. `sonar integrate` is never run (it installs MCP and hooks). |
| C4 | **`sqa` never handles tokens.** Authentication is fully delegated to the official CLI: `sonar auth login` (browser flow or `--with-token`; `sonar` stores the token in sqa's private credentials file — `0600` on Unix, a user-only ACL on Windows — or the OS keychain on request) for interactive use, or `SONARQUBE_CLI_SERVER` + `SONARQUBE_CLI_TOKEN` env vars for CI and headless agents. |
| C5 | **Human-in-the-loop triage.** Agents must ask the user explicitly before any triage decision; once approved, the agent may execute it. |
| C6 | Go, single static binary that **contains the official `sonar` CLI** (one download = everything). Release target for now: **Linux amd64 only**; macOS and arm64 builds are temporarily not provided. |
| C7 | One SonarQube instance (no multi-server profiles). |

## 3. Why not just use `sonar` directly?

The official CLI covers authentication and `sonar list issues` (filters: severity, type, status, branch, PR, file, new code; `--format json|toon|table|csv`) against SonarQube Server. Gaps for our use:

- No security-first ranking across finding types.
- No Security Hotspot commands (hotspots are only exposed via the MCP server — excluded by C3).
- No issue/hotspot status changes outside MCP.
- `sonar integrate` is agent-specific and installs MCP — not allowed.
- `sonar remediate` is SonarQube Cloud only.

`sqa` fills these gaps while delegating issue retrieval to `sonar`.

## 4. Architecture

```
harness (Grok / OpenCode / Claude / …)
        │  reads SKILL.md, runs shell commands
        ▼
      sqa  (Go binary)
        ├── config resolution (~/.config/sqa/config.yaml or %APPDATA%\sqa\config.yaml, no secrets)
        ├── issue reads ──────────► `sonar list issues` ─┐
        ├── hotspots, rules, source,                    ├─► SonarQube Server
        │   status changes ────────► `sonar api get|post` ┘
        │   (all traffic goes through the bundled `sonar` CLI and its auth)
        ├── triage/ranking engine
        ├── audit log (~/.local/state/sqa/audit.jsonl)
        └── embedded SKILL.md
```

### 4.1 Bundling the official `sonar` CLI

- `sqa` pins a tested `sonar` version: **1.12.0.17622** (`sonarcli.PinnedVersion`), with the SHA-256 SonarSource publishes for each platform binary **compiled into `sqa`** (`pinnedChecksums`).
- Official source (the same as SonarSource's own `install.sh` / `install.ps1`): `https://binaries.sonarsource.com/Distribution/sonarqube-cli/<version>/<os>/sonarqube-cli-<version>-<platform>.bin` (`.exe` on Windows), checksum at `<binary>.sha256`. The GitHub releases of `SonarSource/sonarqube-cli` carry no binary assets.
- **Release builds embed it.** `mise run fetch-sonar` / `fetch-sonar-windows` (`tools/fetch-sonar -os/-arch`, reusing `sonarcli.Installer`) downloads the matching official binary to `internal/sonarcli/bundled/sonar-linux-amd64` or `sonar-windows-amd64.exe` (gitignored, never committed) and verifies it against the compiled-in checksum. `mise run build-bundled` and `build-bundled-windows` build with `-tags bundled_sonar`, which compiles `bundled_on.go` or `bundled_on_windows_amd64.go` (`//go:embed` of only that platform's binary). The tag is only valid for linux/amd64 and windows/amd64: on any other target `bundled_unsupported.go` breaks the build on purpose. Without the tag (`go install`, plain `go build`), nothing is embedded and `sqa` behaves as before.
- **Extraction.** The OS can only exec a file, so on first use `sqa` writes the embedded bytes to `~/.local/share/sqa/bin/sonar-<version>` (`XDG_DATA_HOME` honoured), or `%LOCALAPPDATA%\sqa\bin\sonar-<version>.exe` on Windows: hash the embedded bytes, write to a temp file in the same directory, fsync, hash the written file, `chmod 0755`, and rename into place (on Windows, replace an existing file first because `os.Rename` cannot overwrite). A stamp file `sonar-<version>[.exe].verified` records digest, size and mtime; later runs only stat the file and compare (cheap), and re-hash when size or mtime changed, re-extracting on mismatch. The Unix executable-bit check is skipped on Windows. Binaries left by other versions (`sonar-<other>`) are removed. The versioned file name means an upgrade never overwrites a binary another `sqa` process is running.
- **Resolution order:** `sonar.path` from config → bundled (extracted) → `sqa setup` download (`~/.local/share/sqa/bin/sonar`, or `sonar.exe` on Windows) → `sonar` on `PATH`. Setting `sonar.version` in config to a version other than the bundled one skips the bundle.
- **`sqa setup`** is optional with a bundled build: it performs (or with `--force` redoes) the extraction, runs `sonar --version` and reports. `sqa setup --sonar-version <v>` with another version downloads that official release (pinned checksum, or the published `.sha256` for other versions) to the managed path, as for non-bundled builds. SonarSource publishes downloads for linux-x86-64, linux-arm64, macos-arm64 and windows-x86-64 (no macOS x86-64).
- **`sqa version` / `sqa doctor`** report whether `sonar` is bundled, its version and SHA-256, and which source is in use (`sonar.path`, `bundled`, `sqa setup`, `PATH`). `version` never writes anything; `doctor` extracts if needed.
- **Updates.** A new `sonar` means a new `sqa` release. `.github/workflows/sonar-update.yml` runs weekly (and on demand), reads `stable.version` from the distribution URL above (what SonarSource's `install.sh` uses to resolve the latest release), and opens an issue with the new version, binary URL and published checksum when it is newer than `PinnedVersion`. It never commits code or binaries; bumping is a reviewed change to `install.go` and `THIRD_PARTY_NOTICES.md` (a test keeps the two in step).
- `sqa` always invokes this binary (or `sonar.path`) with sqa's token store and `sonar` home (§5), and inherits the `SONARQUBE_CLI_*` env vars, which take precedence over a stored login. `sqa` never reads, stores or prints a token, and never runs `sonar integrate`.
- **Licence.** `sonar` is LGPL-3.0 (Copyright SonarSource Sàrl). It is redistributed unmodified as a separate executable that `sqa` runs as a child process (not linked). Release archives include `THIRD_PARTY_NOTICES.md` (component, version, copyright, upstream source tag, exact binary URL and SHA-256, how to replace it via `sonar.path`) and `licenses/sonarqube-cli-LGPL-3.0.txt` plus the GPL-3.0 text it incorporates.
- `sqa` refuses to run `sonar integrate` and `sonar remediate` (guard in `sonarcli.Runner`).

### 4.2 Web API usage via `sonar api` (only where `sonar` has no dedicated command)

These endpoints are called through `sonar api get|post <path> [--data <json>]`, so they reuse the CLI's authentication, TLS and proxy settings. `sqa` contains no HTTP client of its own.

| Purpose | Endpoint |
|---------|----------|
| Version check | `GET /api/server/version` (plain text) |
| Token check | `sonar auth status --format json` (not an API call by sqa) |
| Single issue by key (`sqa show`) | `GET /api/issues/search?issues=<key>&additionalFields=_all` |
| Hotspots list / detail | `GET /api/hotspots/search` (`status=TO_REVIEW`, and `status=REVIEWED&resolution=ACKNOWLEDGED`; `inNewCodePeriod`), `GET /api/hotspots/show` |
| Hotspot review / reopen | `POST /api/hotspots/change_status` (`REVIEWED` + resolution, or `TO_REVIEW` without one) |
| Hotspot comment / assign | `POST /api/hotspots/add_comment`, `POST /api/hotspots/assign` (internal endpoints used by the SonarQube UI, since 8.1 / 8.2) |
| Quality gate (`findings` summary, `coverage`) | `GET /api/qualitygates/project_status` |
| Coverage measures | `GET /api/measures/component`, `GET /api/measures/component_tree` (`strategy=leaves`, `qualifiers=FIL`, `metricSort=new_uncovered_lines`, `s=metricPeriod`, `metricPeriodSort=1`, `asc=false`; overall: `s=metric`, `metricSort=uncovered_lines`) |
| Issue transition | `POST /api/issues/do_transition` (`falsepositive`, `accept`, `reopen`) |
| Issue comment | `POST /api/issues/add_comment` |
| Rule details | `GET /api/rules/show` |
| Code context, line coverage | `GET /api/sources/lines` (`lineHits`, `conditions`, `coveredConditions`, `isNew`) |

Auth, TLS and proxy: handled by `sonar` (see `sonar config` for proxy/CA settings).

`sonar api` sends `--data` as form data for `/api/...` (v1) endpoints, which is what these endpoints expect. On HTTP errors it exits 1 with `SonarQube API error: <status> - <body>` on stderr; `sqa` maps that to exit code 3.

### 4.3 `sonar` output shapes used

- `sonar --version` prints the short version (`1.12.0`) on stdout (banner on stderr).
- `sonar auth status --format json` prints `{"status": "connected"|"not_authenticated"|"token_missing"|"token_invalid"|"unreachable"|…, "server", "org", "source", "message"}` and exits 1 when not connected.
- `sonar list issues --format json` prints the unmodified `/api/issues/search` response (`total`, `paging`, `issues[]` with `key, rule, severity, type, impacts[{softwareQuality, severity}], issueStatus, component, line, textRange, flows, message`, and `components[]` with `path`). It announces auto-detection on stderr (`Using auto-detected project key: …`), which `sqa` parses so hotspot queries use the same project. Without `--branch`/`--pull-request` it also picks a pull request from the local git branch (`Using pull request N (auto-detected from branch …)`); `sqa` prevents that by always passing `--branch <main branch>` (from `/api/project_branches/list`) or the user's `--branch`/`--pr` (see §5 scope). Pagination: `--page-size 500 --page N`.
- New code: the issues response has no per-issue new-code flag, so `sqa findings` makes a second call with `--new-code` (and `inNewCodePeriod=true` for hotspots) and marks the keys returned. On a pull request every finding counts as new code.

## 5. Authentication and configuration

**Interactive (developer machine):** the user runs `sqa auth login --server https://sonarqube.internal.example`, which simply execs `sonar auth login --server …` attached to the terminal (browser flow, or `--with-token` reading stdin). Agents cannot complete the browser flow, which is fine: the user logs in once, agents then reuse it. `sqa auth logout` passes through likewise. `sqa auth status` runs `sonar auth status --format json` and renders it (exit 3 when not connected).

**Headless (CI, containers):** set `SONARQUBE_CLI_SERVER` and `SONARQUBE_CLI_TOKEN`; these override any stored login.

**Token store.** `sonar` keeps logins in the OS keychain via `Bun.secrets` unless `SONARQUBE_CLI_KEYCHAIN_FILE` names a JSON file (`{"tokens": {"<host>": "<token>"}}`, written with `writeFileSync`). On Linux the keychain needs libsecret plus a running Secret Service, which servers, WSL and SSH sessions usually lack, so `sqa` defaults to the file, like `terraform login` and `oc login`. `sonarcli.Environment` owns this; every `sonar` child process gets it (`Runner.Env`):

- `SONARQUBE_CLI_KEYCHAIN_FILE` = `auth.credentials_file`, default `$XDG_CONFIG_HOME/sqa/credentials.json`. Not set with `auth.store: keychain`.
- `SONAR_USER_HOME` = `sonar.home`, default `$XDG_DATA_HOME/sqa/sonar-home`. sqa's own `state.json` never lists `sonar-secrets` (only `sonar integrate`, `analyze secrets` and the git hooks install it, and sqa runs none of them), so `sonar`'s post-upgrade dependency repair never downloads it. Logins made with a standalone `sonar` in `~/.sonar` are not shared.
- Precedence: a non-empty variable in the user's environment (`sonar` reads it anyway) → `config.yaml` → default. An empty `SONAR_USER_HOME` is replaced, since `sonar` would resolve it against the working directory.
- Permissions, without reading the file: before each run, sqa's own directories are created or tightened to `0700` (a configured or env-provided parent is created `0700` if missing but never chmodded), and the credentials file is created empty with `O_CREAT|O_EXCL`, mode `0600`, if missing (`sonar` treats an unparsable file as empty). `writeFileSync` truncates an existing file in place and keeps its mode, so the token is never in a world-readable file regardless of umask (verified against the bundled 1.12.0.17622). After each run sqa `chmod`s the file to `0600` again in case anything replaced it. Changing the process umask was rejected: it is process-wide in Go and racy with other goroutines, and the pre-created file makes it unnecessary. On Windows, mode bits are not enforced: `sqa` instead sets a protected DACL on the credentials file and on sqa's own directories, granting only the current user SID full control (`golang.org/x/sys/windows` `SetNamedSecurityInfo` with `PROTECTED_DACL_SECURITY_INFORMATION`). `sqa doctor` uses the same check and repair, and does not report Unix modes there.
- `DO_NOT_TRACK=1` unless the user's environment has `DO_NOT_TRACK` at all (passed through as is, even `0` or empty) or `sonar.telemetry: true`. Upstream `isTelemetryEnabled()` (`telemetry.enabled` config and `DO_NOT_TRACK === "1"`) gates event emission, the detached `flush-telemetry` worker, the flush itself and `initSentry()`, so one variable covers usage telemetry (`events.sonardata.io`) and Sentry. The internal `__SQ_CLI_TELEMETRY_EGRESS` switch is not used: it is undocumented and only controls egress, not collection. `doctor` and `version` print the state (`telemetry: off (…)`).
- `sonar` labels a stored login `OS Keychain` even when it comes from the file; `sqa auth status` and `sqa doctor` relabel it `credentials file` and print the store and home. `doctor` reports (and fixes) wrong permissions before running `sonar`, and maps the two Linux keychain errors (`libsecret could not be loaded`, `org.freedesktop.secrets`) to fixes only when `auth.store: keychain`.

**Config file** `~/.config/sqa/config.yaml` (or `%APPDATA%\sqa\config.yaml` on Windows; no secrets, optional):

```yaml
default_project: my-service   # optional; otherwise sonar auto-detects / sonar link
auth:
  store: file                 # file (default) or keychain
  credentials_file: ""        # default $XDG_CONFIG_HOME/sqa/credentials.json
sonar:
  version: "1.12.0.17622"     # pinned official CLI version
  path: ""                    # optional override
  home: ""                    # SONAR_USER_HOME, default $XDG_DATA_HOME/sqa/sonar-home
  telemetry: false            # true: don't set DO_NOT_TRACK=1 for sonar
skills:
  default_dir: ~/.agents/skills
```

Location: `$SQA_CONFIG`, else `$XDG_CONFIG_HOME/sqa/config.yaml`, else `~/.config/sqa/config.yaml` (on Windows, `%APPDATA%\sqa\config.yaml` when no XDG override is set).

Project resolution: `--project` → `default_project` → `sonar` auto-detection (`sonar link`, git remote; one `sonar list issues --page-size 1` call). If none works, `sqa findings` exits 2 asking for `--project`.

Scope: `--branch` or `--pr` when given, otherwise the project's main branch, for every command (findings, next, show, coverage, pr-body and the gate summary). `sqa` never derives a branch or pull request from the local git checkout: the fix loop branches from the default branch, so the main branch is the right default. Web API calls simply omit `branch`/`pullRequest`; `sonar list issues` gets the main branch by name.

Multiple servers: out of scope (one internal instance). `sonar` keeps one active saved connection; switching would be `sqa auth login --server <other>` or per-shell env vars.

## 6. Commands

### Read-only (agents may run freely)

| Command | Description |
|---------|-------------|
| `sqa setup` | Optional: extract/verify the bundled `sonar`, or download another official version. |
| `sqa auth login\|status\|logout` | Thin pass-through to `sonar auth …` (login must be run by the user in a terminal). |
| `sqa doctor` | Check `sonar` installed and version matches (warn on mismatch), authenticated (`sonar auth status`, shows server and source), server ≥ 2025.1 (or Community Build ≥ 25.1). |
| `sqa findings` | Unified, ranked list of open issues (`OPEN`, `CONFIRMED`) + `TO_REVIEW` and `REVIEWED/ACKNOWLEDGED` hotspots, with status and `pr` columns (open fix PR, §6.1), starting with a quality gate summary (§6.2). Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`, `--no-acknowledged`, `--no-pr-check`, `--no-summary`, `--format`. |
| `sqa next` | The highest-priority open finding without an open fix PR, with the same brief as `sqa show` (for one-at-a-time loops). `--skip <key>` (repeatable, comma-separated) passes over findings by hand; `--include-open-pr` turns PR skipping off. Output lists `skipped` (rank, key, reason `open PR` or `--skip`, pr), `total`, `remaining`, `pr_check`, `note`. |
| `sqa pr-body` | `<key>...` or `--from-findings` with the `findings` filters, plus `--reason`: the Markdown "Resolves SonarQube findings" block for a PR body, one line per finding (§6.3). Markdown by default; `--format json` adds the findings. |
| `sqa coverage` | Files ranked by uncovered lines on new code (overall without a new-code period or with `--overall`), with coverage %, uncovered lines and conditions and the gate's coverage condition; `--file` for a file's uncovered line ranges and partly covered conditions, or a directory's ranking; `--top`, `--fail-on <percent>` (§6.2). |
| `sqa show <key>` | Agent-ready brief: rule, rationale (rule description sections `root_cause` / `assess_the_problem` / `how_to_fix`, or hotspot risk text), location, code context (`--context N`, default 5, HTML stripped), data-flow, possible verdicts, next step, and a URL to the finding on the server (from `sonar auth status`; the token is never read). Tries the key as an issue, then as a hotspot. |
| `sqa version` | sqa version, pinned `sonar` version, whether it is bundled (version, SHA-256) and which `sonar` is in use. |

### 6.1 Open fix PR lookup (`findings`, `next`, `show`)

So an agent does not get stuck on (or duplicate) a finding that already has a fix in review, `sqa` asks the **forge**, never SonarQube, and never touches a token (`gh` uses its own login, `git` the repository's credentials). One listing call per invocation, not per finding, each with a 15 s timeout and prompts disabled (`GH_PROMPT_DISABLED=1`, `GIT_TERMINAL_PROMPT=0`):

1. `gh pr list --state open --limit 200 --json number,url,headRefName,body` in the current directory. A finding has an open PR when its key appears as a whole token in a PR body (the skill puts the SonarQube URL there) or a PR head branch ends with the key after lowercasing and collapsing non-alphanumerics to `-` (the skill's `sonar/<rule>-<key>`). The label is not required, so PRs without the `sonar` label still count.
2. If `gh` is missing, not logged in, or the remote is not GitHub: `git ls-remote --heads origin 'refs/heads/sonar/*'`, matched by branch name only. The reference is then `branch:<name>` and the note says a branch may have no open PR (works for GitLab and others).
3. If both fail: nothing is skipped, `pr_check` is `unavailable` and `note` carries both errors. The command still succeeds.

Every finding is looked up on its own, so one PR body listing several keys (a grouped PR, e.g. an exclusion change) marks all of them, and `next` skips all of them. A branch name only carries one key, so with the `git` fallback only that one is recognised.

`pr_check` values: `gh`, `git`, `unavailable`, `off` (`--no-pr-check`), `none` (no findings, so no lookup). The `pr` field holds the PR URL (or `branch:<name>`), empty otherwise; the table shows `#12`, `!12` (GitLab MR) or `branch`.

### 6.3 PR bodies (`sqa pr-body`) and `--file` globs

`sqa pr-body` reads each key with `GetIssue`, falling back to the hotspot (`/api/hotspots/show`); an unknown key is a usage error. With `--from-findings` it runs the same gather as `findings` (without the forge lookup) and lists every match. Rule names come from `sonar api` `/api/rules/show`, cached per run; links use the server from `sonar auth status`. Output is

```
### Resolves SonarQube findings

Resolved by: <reason>        (only with --reason)

- [<message>](<SonarQube URL>) (`<rule>` <rule name> · `<file>:<line>` · key `<key>`)
```

The key is in each line so §6.1 matches every listed finding.

`--file` (findings, next, pr-body): a value containing `*`, `?` or `[` is a glob. It is not sent to the server; sqa fetches the unfiltered list and keeps findings whose path matches (`*`/`?` within a segment, `**` across directories, `**/` also matches no directory). A plain path is passed to `sonar list issues --file` / hotspot `files` unchanged (a file, or a directory's own files).

### 6.2 Coverage (`sqa coverage`) and the `findings` summary

`sonar` 1.12 has no coverage command; `sonar quality-gate status --category coverage` lists worst-covered files only for an already failing coverage condition and has no line detail. Coverage is therefore its own read-only verb, not a finding type: it has no key, status or triage verdict, does not fit the security-first order, and only changes after the next analysis that imports a coverage report.

- `sqa coverage` resolves the project and scope like `findings` (§5). It reads the scope's measures (`/api/measures/component`; `new_coverage`, `new_lines_to_cover`, `new_uncovered_lines`, `new_uncovered_conditions` and the overall ones), the gate (`/api/qualitygates/project_status`; condition on `new_coverage` or `coverage`), and the files below the scope sorted by uncovered lines (`/api/measures/component_tree`). New-code values come from the measure's `period.value` (or `periods[0].value` on older servers).
- Scope: new code, unless the project has no `new_lines_to_cover` measure (no new-code period; a note says so) or `--overall`. Fully covered files are dropped from the ranking. `--top N` (default 20, at most 500) is the page size.
- `--file <path>`: the component `<project>:<path>`. A directory ranks the files below it; a file reads `/api/sources/lines` for the whole file and reports uncovered line ranges (`lineHits == 0`) and lines with `coveredConditions < conditions`, limited to `isNew` lines in the new-code scope. Lines without `lineHits` are not executable or not covered by the report; if no line has coverage data, a note says so. An unknown path is a usage error (exit 2).
- `--fail-on <percent>` exits 1 when the scope's coverage is below it (no coverage value: no failure).
- `sqa findings` and `sqa next` start with the gate read-out (`summary` / `gate`, the first JSON/TOON field, printed above the findings in table and Markdown): one `/api/qualitygates/project_status` call for the gate status and the conditions on `new_coverage`/`coverage` and `new_security_hotspots_reviewed`/`security_hotspots_reviewed`; only if the gate has no coverage condition, one `/api/measures/component` call for the coverage value. Errors become `summary.note`; `findings` never fails because of it. Each failing coverage or hotspots-reviewed condition (or a failing gate otherwise) adds a `next_steps` entry telling the agent what to report; `next` appends them to its `hint`, also when the queue is empty. `--no-summary` skips the calls.

### State-changing (require user approval — see §8)

| Command | Description |
|---------|-------------|
| `sqa mark <issue-key> fp\|accept\|reopen --comment "…" --approved-by-user` | `do_transition` (`falsepositive`, `accept`, `reopen`), then `add_comment`. |
| `sqa hotspot <hotspot-key> safe\|fixed\|acknowledged --comment "…" --approved-by-user` | Review a hotspot (status REVIEWED + resolution). |
| `sqa hotspot <hotspot-key> reopen --comment "…" --approved-by-user` | Back to TO_REVIEW (`change_status` without resolution). |
| `sqa hotspot <hotspot-key> comment --comment "…" --approved-by-user` | Comment only (`add_comment`). |
| `sqa hotspot <hotspot-key> assign <login> --comment "…" --approved-by-user` | Assign (`assign`, with the comment); the server allows it only on TO_REVIEW or ACKNOWLEDGED hotspots. |

All of them share the approval rules of §8: `--approved-by-user` and a non-empty `--comment` (exit 4 otherwise), `[sqa]` comment prefix, `--dry-run`, and an audit log line per attempt (action e.g. `reopen`, `comment`, `assign alice`).

### Skill

| Command | Description |
|---------|-------------|
| `sqa skill print` | Print the embedded `SKILL.md`. |
| `sqa skill install [--target agents\|claude\|opencode\|cursor\|<path>] [--project]` | Write `sqa/SKILL.md`. Default target `agents` = `~/.agents/skills/sqa/` (or `skills.default_dir`; read by Codex, Cursor, OpenCode, Pi and others). `claude` = `~/.claude/skills/sqa/` (Claude Code does not read `~/.agents/skills`). `opencode` = `~/.config/opencode/skills/sqa/`. `cursor` = `~/.cursor/skills/sqa/`. A path writes `<path>/sqa/SKILL.md`. `--project` writes the project-scoped variant (`./.agents/skills/sqa/`, `./.claude/skills/sqa/`, `./.opencode/skills/sqa/`, `./.cursor/skills/sqa/`). Never touches MCP config. |

### Output & exit codes

- `--format toon` (default when stdout is not a TTY; token-efficient), `json`, `md`, `table`.
- Exit `0` success / nothing above threshold; `1` findings above threshold (for `findings --fail-on`); `2` usage/config error; `3` auth/server error; `4` approval missing.

## 7. Prioritisation (security first)

Every finding is normalised to `{rank, key, kind(issue|hotspot), tier, category, quality, severity, rule, file, line, new_code, status, message}` and ranked:

1. **Tier 1 – Vulnerabilities**: issues with software quality `SECURITY` (MQR mode) / type `VULNERABILITY`.
2. **Tier 2 – Security Hotspots** with status `TO_REVIEW` or reviewed as `ACKNOWLEDGED` (confirmed risk, fix pending), ordered by vulnerability probability HIGH → LOW like the other tiers; `--no-acknowledged` drops the latter.
3. **Tier 3 – Reliability**: `RELIABILITY` / `BUG`.
4. **Tier 4 – Maintainability**: `MAINTAINABILITY` / `CODE_SMELL`.

Within a tier: severity (BLOCKER > HIGH > MEDIUM > LOW > INFO; standard mode CRITICAL→HIGH, MAJOR→MEDIUM, MINOR→LOW) → new code before overall code → file path → line → key. Both MQR and Standard Experience severities are supported and mapped to one scale.

MQR issues can carry several impacts; the most security-relevant impact decides the tier and its severity is used (an issue with a LOW `SECURITY` impact and a BLOCKER `MAINTAINABILITY` impact is a tier 1 LOW). Issues without impacts fall back to `type` and `severity`. Hotspot severity is its vulnerability probability.

## 8. Human-in-the-loop triage

Principle: **the agent recommends, the user decides, then the agent executes.**

Workflow the skill enforces:

1. Agent runs `sqa next` (includes the brief). Use `sqa findings` for an overview and `sqa show <key>` for a named key.
2. Agent presents each finding to the user with a **recommended verdict** (fix in code, false positive, accept, or for hotspots safe / fixed / acknowledged) and the reasoning.
3. Agent **stops and asks explicitly**. No action without a clear yes for that specific finding.
4. After approval:
   - *Fix* → the agent edits code in its normal flow (reviewed by the user as usual).
   - *FP / accept / hotspot review* → agent runs the state-changing command with `--approved-by-user` and a comment that reflects the user's reasoning.
5. Approval is scoped to the findings the user named. A general "go ahead" does not extend to findings that were not presented.

CLI-side guardrails:

- State-changing commands refuse to run without `--approved-by-user` and a non-empty `--comment` (exit 4).
- Every attempt is appended to `~/.local/state/sqa/audit.jsonl` (file mode 0600; fields: time (UTC), os_user, kind, key, action, comment, dry_run, result `ok|error|dry-run`, error). The SonarQube comment is prefixed with `[sqa]` for traceability on the server side.
- `--dry-run` shows what would change without calling the server. It still requires approval and a comment, so the guardrail is uniform.
- Approval is checked before `sonar` is even resolved: a refused command never reaches the server.

## 9. Embedded SKILL.md (outline)

Frontmatter: `name: sqa`, `description: …`. Content:

- What `sqa` is; prerequisite: `sqa doctor` must pass; the user (not the agent) runs `sqa auth login`.
- **Hard rules**: never use, install or configure a SonarQube MCP server; never run `sonar integrate` or `sonar api`; never run `sonar auth login` (the user does that); never set, read or print tokens; always ask the user before any triage decision.
- Triage order (§7) and how to read `next` / `findings` / `show` output.
- The fix loop: `sqa next` skips findings with an already-open PR/MR (forge lookup, see §6.1); if a finding still shows a `pr`, or the lookup was `unavailable`/`git`, check the forge, do not duplicate and ask the user how to proceed; `--skip <key>` only when the user wants to leave a finding for now → one finding per branch/PR from the up-to-date default branch → fix and commit (Conventional Commits, include the rule key) → tests → open a PR (ask first; never merge; body carries the `sqa pr-body` block listing every finding the PR resolves, including findings resolved indirectly; apply the `sonar` label) → return to the default branch → (re-scan via CI or `sonar` analysis if available) → re-check with `sqa show <key>` / `sqa next`. Presentation: if the finding is in test code, accept or a Sonar exclusion may be appropriate; never edit Sonar settings without approval.
- Acknowledged hotspots: risk confirmed, fix pending; recommend the fix, or ask about `reopen` / `safe`. Hotspot comments and assignments are visible writes and need approval of the exact text/login.
- Findings a PR resolves: one line each via `sqa pr-body`; grouping several findings in one PR needs explicit approval first; never add an unrelated fix silently. Config fixes (exclusions, test inclusions, coverage exclusions): list what they remove with `sqa findings --file '<glob>'`, get approval for the grouped PR, put `sqa pr-body --from-findings --file '<glob>' --reason "…"` in the body.
- Coverage: when the summary shows failing coverage or the user asks, `sqa coverage` then `sqa coverage --file`; writing tests is normal code work under the fix loop (ask before branching/PRs, one PR per change, meaningful assertions, never change coverage settings or thresholds without approval).
- How to present a recommendation and ask for approval (template), and that approval covers only the presented findings.
- Never mark something FP/accepted just to reduce counts; justify every decision in the comment.

## 10. Project layout

```
sqa/
├── cmd/sqa/main.go
├── internal/
│   ├── cli/         # cobra commands, exit codes
│   ├── config/      # config file, XDG directories
│   ├── sonarcli/    # pinned sonar: bundle/extract, download, resolve, invocation, output parsing
│   │   └── bundled/ # sonar-linux-amd64 or sonar-windows-amd64.exe fetched at build time (gitignored)
│   ├── findings/    # normalisation + ranking
│   ├── forge/       # open fix PR lookup via gh / git ls-remote (§6.1)
│   ├── approval/    # guardrails + audit log
│   └── output/      # toon/json/md/table renderers
├── skill/SKILL.md   # embedded via go:embed
├── install.sh       # curl | sh installer (checksum-verified, linux/amd64)
├── tools/fetch-sonar/ # download + verify the pinned sonar for bundling
├── licenses/        # LGPL-3.0 / GPL-3.0 texts for the bundled sonar
├── THIRD_PARTY_NOTICES.md
├── mise.toml        # pinned Go, golangci-lint, shellcheck; tasks (build, test, lint, lint-sh, …)
├── mise.lock        # resolved tool versions + checksums
├── testdata/fake/   # fake `sonar` script + recorded API responses
├── testdata/forge/  # fake `gh` and `git` for the open-PR lookup tests
├── testdata/install/  # install.sh tests and a fake GitHub Releases API
├── .github/         # CI, CodeQL, release, sonar-update workflows, dependabot
└── DESIGN.md
```

Libraries: `cobra` (commands), `gopkg.in/yaml.v3`, `golang.org/x/term`; no HTTP client for SonarQube, all server traffic via `sonar` (`net/http` is used only by `sqa setup` and `tools/fetch-sonar` to download the `sonar` binary). Tests run a fake `sonar` shell script (`testdata/fake/sonar`) that serves recorded responses and logs every invocation, so they assert on the exact `sonar` arguments (e.g. that no `api post` happens without approval).

## 10a. Build, CI and release

- **Tools and tasks**: `mise.toml` pins Go, golangci-lint and shellcheck (`mise.lock` records versions, download URLs and checksums) and defines the tasks (`build`, `build-bundled`, `fetch-sonar`, `sonar-version`, `test`, `test-install`, `test-bundled`, `lint`, `lint-sh`, `vulncheck`, `clean`). Every workflow sets up tools with `jdx/mise-action` (pinned mise version, binary verified against mise's signed checksums, `mise install --locked`, tool installs cached) and runs the same `mise run …` tasks as a developer. Go module and build caches (`~/go/pkg/mod`, `~/.cache/go-build`, plus `~/.cache/golangci-lint` for lint) are kept with `actions/cache` under one key per OS, `go.sum` and `mise.lock`, so a run saves them once and later runs restore them.
- **CI** (`.github/workflows/ci.yml`, push/PR to `main`) is a single job so setup runs once; each check after the first runs even if an earlier one failed (`if: !cancelled()`): `mise run lint` (`go vet` + golangci-lint + shellcheck), `mise run test` with coverage, `mise run test-install`, an untagged linux/amd64 `mise run build`, then the bundled steps: restore the `sonar` binary from `actions/cache` (key: pinned version from `mise run sonar-version`), `mise run test-bundled` (runs `fetch-sonar` first, which downloads on a miss and re-verifies the SHA-256 either way; then `go vet` and `go test -race` with `-tags bundled_sonar`, including a test that the embedded bytes match the pinned checksum and that the extracted `sonar --version` runs), `mise run build-bundled`, smoke test (`sqa version`, `sqa setup`, `sqa doctor` must fail only on auth). `mise run vulncheck` (govulncheck).
- **CodeQL** (`codeql.yml`): Go, on push/PR and weekly; Go comes from mise, manual build mode with `go build ./...`.
- **Release** (`release.yml`, on tag `v*`): `mise run test`, then one linux/amd64 job: fresh `mise run fetch-sonar` (no sonar cache), embedded-binary test, `mise run build-bundled` with `VERSION=<tag> COMMIT=<sha>` (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X …/internal/cli.version=<tag> -X …/internal/cli.commit=<sha>"`, `-tags bundled_sonar`), smoke test, and packaging of `sqa_<version>_linux_amd64.tar.gz` (single root dir: `sqa`, `LICENSE`, `README.md`, `THIRD_PARTY_NOTICES.md`, `licenses/`; reproducible tar flags). A final job generates an SPDX SBOM, writes `checksums.txt` (`sha256sum`), creates build provenance attestations (`actions/attest-build-provenance`) for the archives and checksums, and publishes with `gh release create --generate-notes`. The asset name contains `linux` and `amd64`, which is what mise's `github:` backend matches. No goreleaser.
- **sonar update check** (`sonar-update.yml`): weekly + manual, `contents: read` at the top, `issues: write` only on its single job; opens an issue when `stable.version` is newer than the pinned version (see §4.1).
- **Hardening**: every action pinned to a full commit SHA with a version comment, `permissions: contents: read` at the top, `contents: write` / `id-token: write` / `attestations: write` only on the release job, `persist-credentials: false`, `step-security/harden-runner` (egress audit), timeouts, concurrency groups. Dependabot updates Go modules and actions weekly; Go is pinned once, by the `toolchain` line in `go.mod`, which mise reads (`idiomatic_version_file_enable_tools = ["go"]`). The mise version in the workflows and golangci-lint in `mise.toml` are bumped by hand, then `mise lock`.

## 11. Decisions and open questions

Resolved:

1. Licence and distribution: `sonar` (LGPL-3.0) is bundled unmodified inside the release binary, with `THIRD_PARTY_NOTICES.md` and the licence texts in every archive (§4.1). `sqa setup` remains for non-bundled builds and version overrides.
2. Pinned `sonar` version: 1.12.0.17622 (stable on 2026-11-17).
3. Multiple servers: not needed (one internal instance).
4. Name: `sqa`.
5. Platforms: release builds are Linux amd64 and Windows amd64; Windows (`install.ps1`, the bundled `sonar.exe`, the credentials-file DACL) was confirmed on real hardware with an earlier release. macOS and arm64 are not provided for now.

Open:

1. Skill install path for Grok and any internal harnesses (default `~/.agents/skills` covers Codex, Cursor, OpenCode, Pi; `--target <path>` covers the rest).
2. Should `sqa` also trigger analysis (`sonar analyze`) or rely on CI scans only? (Currently CI only.)
3. JSON shapes were built from the `sonar` CLI source and documentation and exercised against the real `sonar` 1.12.0 binary with a stub server; they still need a run against the internal SonarQube instance.
