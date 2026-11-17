# sqa — SonarQube Agent CLI: Design

Status: Implemented in v0.1.0 · Date: 2026-11-17 · Owner: Dan Sailer

## 1. Goal

A single-binary Go CLI (`sqa`) that lets coding harnesses (Grok, OpenCode, Claude Code, Cursor, …) read findings from internally hosted SonarQube Server, prioritised **security first**, and help fix them — with the **user always in control of triage decisions**. The CLI ships its own harness-neutral `SKILL.md` that any harness can install.

## 2. Constraints

| # | Constraint |
|---|------------|
| C1 | Targets internally hosted **SonarQube Server 2025.1+**. |
| C2 | **Contains / builds on the official SonarSource `sonar` CLI** ([SonarSource/sonarqube-cli](https://github.com/SonarSource/sonarqube-cli)). |
| C3 | **No MCP.** Must not install, configure or call any SonarQube MCP server. `sonar integrate` is never run (it installs MCP and hooks). |
| C4 | **`sqa` never handles tokens.** Authentication is fully delegated to the official CLI: `sonar auth login` (browser flow or `--with-token`, stored in the OS keychain) for interactive use, or `SONARQUBE_CLI_SERVER` + `SONARQUBE_CLI_TOKEN` env vars for CI and headless agents. |
| C5 | **Human-in-the-loop triage.** Agents must ask the user explicitly before any triage decision; once approved, the agent may execute it. |
| C6 | Go, single static binary (macOS + Linux, amd64 + arm64). |
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
        ├── config resolution (~/.config/sqa/config.yaml, no secrets)
        ├── issue reads ──────────► `sonar list issues` ─┐
        ├── hotspots, rules, source,                    ├─► SonarQube Server
        │   status changes ────────► `sonar api get|post` ┘
        │   (all traffic goes through the bundled `sonar` CLI and its auth)
        ├── triage/ranking engine
        ├── audit log (~/.local/state/sqa/audit.jsonl)
        └── embedded SKILL.md
```

### 4.1 Bundling the official `sonar` CLI

- `sqa` pins a tested `sonar` version: **1.12.0.17622** (`sonarcli.PinnedVersion`, overridable via `sonar.version` in config or `sqa setup --sonar-version`).
- `sqa setup` downloads that release from SonarSource's official distribution, the same source as SonarSource's own `install.sh`:
  `https://binaries.sonarsource.com/Distribution/sonarqube-cli/<version>/<os>/sonarqube-cli-<version>-<platform>.bin`
  with `<os>/<platform>` = `linux/linux-x86-64`, `linux/linux-arm64`, `macos/macos-arm64`. SonarSource publishes **no macOS x86-64 build**; on Intel Macs `sonar.path` must point to a `sonar` installed another way.
- Checksums: SonarSource publishes `<binary>.sha256` next to each binary. For the pinned version, the three SHA-256 digests are **compiled into `sqa`**, so a compromised download host cannot serve a matching binary and checksum pair. For any other version, `sqa` fetches the published `.sha256`. The binary is written to a temp file, verified, made executable and atomically renamed to `~/.local/share/sqa/bin/sonar` (XDG_DATA_HOME honoured).
- Binary resolution order: `sonar.path` from config → managed install → `sonar` on `PATH`.
- The GitHub releases of `SonarSource/sonarqube-cli` carry no binary assets; the CDN above is the official source.
- `sqa` always invokes this pinned binary (or `sonar.path` from config) and inherits whatever authentication `sonar` has: the keychain login from `sonar auth login`, or the `SONARQUBE_CLI_*` env vars, which take precedence. `sqa` never reads, stores or prints a token, and never runs `sonar integrate`.
- Licence: `sonar` is LGPL-3.0. `sqa` does not redistribute it; it is downloaded on `sqa setup`.
- `sqa` refuses to run `sonar integrate` and `sonar remediate` (guard in `sonarcli.Runner`).

### 4.2 Web API usage via `sonar api` (only where `sonar` has no dedicated command)

These endpoints are called through `sonar api get|post <path> [--data <json>]`, so they reuse the CLI's authentication, TLS and proxy settings. `sqa` contains no HTTP client of its own.

| Purpose | Endpoint |
|---------|----------|
| Version check | `GET /api/server/version` (plain text) |
| Token check | `sonar auth status --format json` (not an API call by sqa) |
| Single issue by key (`sqa show`) | `GET /api/issues/search?issues=<key>&additionalFields=_all` |
| Hotspots list / detail | `GET /api/hotspots/search` (`status=TO_REVIEW`, `inNewCodePeriod`), `GET /api/hotspots/show` |
| Hotspot review | `POST /api/hotspots/change_status` |
| Issue transition | `POST /api/issues/do_transition` (`falsepositive`, `accept`, `reopen`) |
| Issue comment | `POST /api/issues/add_comment` |
| Rule details | `GET /api/rules/show` |
| Code context | `GET /api/sources/lines` |

Auth, TLS and proxy: handled by `sonar` (see `sonar config` for proxy/CA settings).

`sonar api` sends `--data` as form data for `/api/...` (v1) endpoints, which is what these endpoints expect. On HTTP errors it exits 1 with `SonarQube API error: <status> - <body>` on stderr; `sqa` maps that to exit code 3.

### 4.3 `sonar` output shapes used

- `sonar --version` prints the short version (`1.12.0`) on stdout (banner on stderr).
- `sonar auth status --format json` prints `{"status": "connected"|"not_authenticated"|"token_missing"|"token_invalid"|"unreachable"|…, "server", "org", "source", "message"}` and exits 1 when not connected.
- `sonar list issues --format json` prints the unmodified `/api/issues/search` response (`total`, `paging`, `issues[]` with `key, rule, severity, type, impacts[{softwareQuality, severity}], issueStatus, component, line, textRange, flows, message`, and `components[]` with `path`). It announces auto-detection on stderr (`Using auto-detected project key: …`, `Using pull request N (auto-detected from branch …)`), which `sqa` parses so hotspot queries use the same project and PR. Pagination: `--page-size 500 --page N`.
- New code: the issues response has no per-issue new-code flag, so `sqa findings` makes a second call with `--new-code` (and `inNewCodePeriod=true` for hotspots) and marks the keys returned. On a pull request every finding counts as new code.

## 5. Authentication and configuration

**Interactive (developer machine):** the user runs `sqa auth login --server https://sonarqube.internal.example`, which simply execs `sonar auth login --server …` attached to the terminal (browser flow; token stored in the OS keychain). Agents cannot complete the browser flow, which is fine: the user logs in once, agents then reuse it. `sqa auth logout` passes through likewise. `sqa auth status` runs `sonar auth status --format json` and renders it (exit 3 when not connected).

**Headless (CI, containers):** set `SONARQUBE_CLI_SERVER` and `SONARQUBE_CLI_TOKEN`; these override the keychain login.

**Config file** `~/.config/sqa/config.yaml` (no secrets, optional):

```yaml
default_project: my-service   # optional; otherwise sonar auto-detects / sonar link
sonar:
  version: "1.12.0.17622"     # pinned official CLI version
  path: ""                    # optional override
skills:
  default_dir: ~/.agents/skills
```

Location: `$SQA_CONFIG`, else `$XDG_CONFIG_HOME/sqa/config.yaml`, else `~/.config/sqa/config.yaml`.

Project resolution: `--project` → `default_project` → `sonar` auto-detection (`sonar link`). If none works, `sqa findings` exits 2 asking for `--project`.

Multiple servers: out of scope (one internal instance). `sonar` keeps one active saved connection; switching would be `sqa auth login --server <other>` or per-shell env vars.

## 6. Commands

### Read-only (agents may run freely)

| Command | Description |
|---------|-------------|
| `sqa setup` | Install/verify the pinned `sonar` CLI. |
| `sqa auth login\|status\|logout` | Thin pass-through to `sonar auth …` (login must be run by the user in a terminal). |
| `sqa doctor` | Check `sonar` installed and version matches (warn on mismatch), authenticated (`sonar auth status`, shows server and source), server ≥ 2025.1 (or Community Build ≥ 25.1). |
| `sqa findings` | Unified, ranked list of open issues (`OPEN`, `CONFIRMED`) + `TO_REVIEW` hotspots. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`, `--format`. |
| `sqa next` | The single highest-priority open finding (for one-at-a-time loops). |
| `sqa show <key>` | Agent-ready brief: rule, rationale (rule description sections `root_cause` / `assess_the_problem` / `how_to_fix`, or hotspot risk text), location, code context (`--context N`, default 5, HTML stripped), data-flow, possible verdicts, next step. Tries the key as an issue, then as a hotspot. |
| `sqa version` | sqa version and the pinned `sonar` version. |

### State-changing (require user approval — see §8)

| Command | Description |
|---------|-------------|
| `sqa mark <issue-key> fp\|accept\|reopen --comment "…" --approved-by-user` | `do_transition` (`falsepositive`, `accept`, `reopen`), then `add_comment`. |
| `sqa hotspot <hotspot-key> safe\|fixed\|acknowledged --comment "…" --approved-by-user` | Review a hotspot (status REVIEWED + resolution). |

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
2. **Tier 2 – Security Hotspots** with status `TO_REVIEW` (ordered by vulnerability probability HIGH → LOW).
3. **Tier 3 – Reliability**: `RELIABILITY` / `BUG`.
4. **Tier 4 – Maintainability**: `MAINTAINABILITY` / `CODE_SMELL`.

Within a tier: severity (BLOCKER > HIGH > MEDIUM > LOW > INFO; standard mode CRITICAL→HIGH, MAJOR→MEDIUM, MINOR→LOW) → new code before overall code → file path → line → key. Both MQR and Standard Experience severities are supported and mapped to one scale.

MQR issues can carry several impacts; the most security-relevant impact decides the tier and its severity is used (an issue with a LOW `SECURITY` impact and a BLOCKER `MAINTAINABILITY` impact is a tier 1 LOW). Issues without impacts fall back to `type` and `severity`. Hotspot severity is its vulnerability probability.

## 8. Human-in-the-loop triage

Principle: **the agent recommends, the user decides, then the agent executes.**

Workflow the skill enforces:

1. Agent runs `sqa findings` / `sqa next` and `sqa show <key>`.
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
- **Hard rules**: never use, install or configure a SonarQube MCP server; never run `sonar integrate`; never run `sonar auth login` (the user does that); never set, read or print tokens; always ask the user before any triage decision.
- Triage order (§7) and how to read `findings` / `show` output.
- The fix loop: pick approved finding → fix → run tests → (re-scan via CI or `sonar` analysis if available) → re-check with `sqa show <key>`.
- How to present a recommendation and ask for approval (template), and that approval covers only the presented findings.
- Never mark something FP/accepted just to reduce counts; justify every decision in the comment.

## 10. Project layout

```
sqa/
├── cmd/sqa/main.go
├── internal/
│   ├── cli/         # cobra commands, exit codes
│   ├── config/      # config file, XDG directories
│   ├── sonarcli/    # pinned sonar install + invocation (list issues, api get/post, auth), output parsing
│   ├── findings/    # normalisation + ranking
│   ├── approval/    # guardrails + audit log
│   └── output/      # toon/json/md/table renderers
├── skill/SKILL.md   # embedded via go:embed
├── testdata/fake/   # fake `sonar` script + recorded API responses
├── .github/         # CI, CodeQL, release workflows, dependabot
└── DESIGN.md
```

Libraries: `cobra` (commands), `gopkg.in/yaml.v3`, `golang.org/x/term`; no HTTP client for SonarQube, all server traffic via `sonar` (`net/http` is used only by `sqa setup` to download the `sonar` binary). Tests run a fake `sonar` shell script (`testdata/fake/sonar`) that serves recorded responses and logs every invocation, so they assert on the exact `sonar` arguments (e.g. that no `api post` happens without approval).

## 10a. Build, CI and release

- **CI** (`.github/workflows/ci.yml`, push/PR to `main`): golangci-lint (action cache), `go vet`, `go test -race` with coverage on Linux and macOS, cross-compile check for all four targets, `govulncheck`. `setup-go` module/build cache on every job.
- **CodeQL** (`codeql.yml`): Go, on push/PR and weekly.
- **Release** (`release.yml`, on tag `v*`): tests, then a 2×2 matrix (`linux`/`darwin` × `amd64`/`arm64`) builds with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X …/internal/cli.version=<tag> -X …/internal/cli.commit=<sha>"`, packages `sqa_<version>_<os>_<arch>.tar.gz` (binary, LICENSE, README; reproducible tar flags) and uploads it as a workflow artifact. A final job downloads all archives, generates an SPDX SBOM, writes `checksums.txt` (`sha256sum`), creates build provenance attestations (`actions/attest-build-provenance`) for the archives and checksums, and publishes with `gh release create --generate-notes`. No goreleaser.
- **Hardening**: every action pinned to a full commit SHA with a version comment, `permissions: contents: read` at the top, `contents: write` / `id-token: write` / `attestations: write` only on the release job, `persist-credentials: false`, `step-security/harden-runner` (egress audit), timeouts, concurrency groups. Dependabot updates Go modules and actions weekly.

## 11. Decisions and open questions

Resolved:

1. Licence: `sonar` is LGPL-3.0; `sqa` keeps download-on-setup and does not redistribute it.
2. Pinned `sonar` version: 1.12.0.17622 (stable on 2026-11-17).
3. Multiple servers: not needed (one internal instance).
4. Name: `sqa`.

Open:

1. Skill install path for Grok and any internal harnesses (default `~/.agents/skills` covers Codex, Cursor, OpenCode, Pi; `--target <path>` covers the rest).
2. Should `sqa` also trigger analysis (`sonar analyze`) or rely on CI scans only? (Currently CI only.)
3. JSON shapes were built from the `sonar` CLI source and documentation and exercised against the real `sonar` 1.12.0 binary with a stub server; they still need a run against the internal SonarQube instance.
