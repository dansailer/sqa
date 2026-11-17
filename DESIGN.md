# sqa — SonarQube Agent CLI: Design

Status: Draft · Date: 2026-11-17 · Owner: Dan Sailer

## 1. Goal

A single-binary Go CLI (`sqa`, working name) that lets coding harnesses (Grok, OpenCode, Claude Code, Cursor, …) read findings from internally hosted SonarQube Server, prioritised **security first**, and help fix them — with the **user always in control of triage decisions**. The CLI ships its own harness-neutral `SKILL.md` that any harness can install.

## 2. Constraints

| # | Constraint |
|---|------------|
| C1 | Targets internally hosted **SonarQube Server 2025.1+**. |
| C2 | **Contains / builds on the official SonarSource `sonar` CLI** ([SonarSource/sonarqube-cli](https://github.com/SonarSource/sonarqube-cli)). |
| C3 | **No MCP.** Must not install, configure or call any SonarQube MCP server. `sonar integrate` is never run (it installs MCP and hooks). |
| C4 | **`sqa` never handles tokens.** Authentication is fully delegated to the official CLI: `sonar auth login` (browser flow or `--with-token`, stored in the OS keychain) for interactive use, or `SONARQUBE_CLI_SERVER` + `SONARQUBE_CLI_TOKEN` env vars for CI and headless agents. |
| C5 | **Human-in-the-loop triage.** Agents must ask the user explicitly before any triage decision; once approved, the agent may execute it. |
| C6 | Go, single static binary (macOS + Linux). |

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
        ├── config/profile resolution (~/.config/sqa/config.yaml + env token)
        ├── issue reads ──────────► `sonar list issues` ─┐
        ├── hotspots, rules, source,                    ├─► SonarQube Server
        │   status changes ────────► `sonar api get|post` ┘
        │   (all traffic goes through the bundled `sonar` CLI and its auth)
        ├── triage/ranking engine
        ├── audit log (~/.local/state/sqa/audit.jsonl)
        └── embedded SKILL.md
```

### 4.1 Bundling the official `sonar` CLI

- `sqa` pins a tested `sonar` version (constant in the build, overridable via config).
- `sqa setup` downloads that release for the current OS/arch from the official SonarSource release source, verifies its checksum, and installs it to `~/.local/share/sqa/bin/sonar`.
- `sqa` always invokes this pinned binary (or `sonar.path` from config) and inherits whatever authentication `sonar` has: the keychain login from `sonar auth login`, or the `SONARQUBE_CLI_*` env vars, which take precedence. `sqa` never reads, stores or prints a token, and never runs `sonar integrate`.
- Open point: whether redistribution inside our own release artefacts is permitted by the `sonar` licence; until confirmed, download-on-setup is the default.

### 4.2 Web API usage via `sonar api` (only where `sonar` has no dedicated command)

These endpoints are called through `sonar api get|post <path> [--data <json>]`, so they reuse the CLI's authentication, TLS and proxy settings. `sqa` contains no HTTP client of its own.

| Purpose | Endpoint |
|---------|----------|
| Version / health check | `GET /api/server/version`, `GET /api/system/status` |
| Token check | `GET /api/authentication/validate`, `GET /api/users/current` |
| Hotspots list / detail | `GET /api/hotspots/search`, `GET /api/hotspots/show` |
| Hotspot review | `POST /api/hotspots/change_status` |
| Issue transition | `POST /api/issues/do_transition` (`falsepositive`, `accept`, `reopen`) |
| Issue comment | `POST /api/issues/add_comment` |
| Rule details | `GET /api/rules/show` |
| Code context | `GET /api/sources/lines` |

Auth, TLS and proxy: handled by `sonar` (see `sonar config` for proxy/CA settings).

## 5. Authentication and configuration

**Interactive (developer machine):** the user runs `sqa auth login --server https://sonarqube.internal.example`, which simply execs `sonar auth login --server …` (browser flow; token stored in the OS keychain under service `sonarqube-cli`). Agents cannot complete the browser flow, which is fine: the user logs in once, agents then reuse it. `sqa auth status` wraps `sonar auth status --format json`.

**Headless (CI, containers):** set `SONARQUBE_CLI_SERVER` and `SONARQUBE_CLI_TOKEN`; these override the keychain login.

**Config file** `~/.config/sqa/config.yaml` (no secrets, optional):

```yaml
default_project: my-service   # optional; otherwise sonar auto-detects / sonar project link
sonar:
  version: "1.x.y"            # pinned official CLI version
  path: ""                    # optional override
skills:
  default_dir: ~/.agents/skills
```

Project resolution: `--project` → `default_project` → `sonar` auto-detection (`sonar project link`).

Multiple servers: `sonar` keeps one active saved connection (keychain account = hostname). Open question whether `sonar` can select a saved connection per command; until then, switching is `sqa auth login --server <other>` or per-shell env vars.

## 6. Commands

### Read-only (agents may run freely)

| Command | Description |
|---------|-------------|
| `sqa setup` | Install/verify the pinned `sonar` CLI. |
| `sqa auth login\|status\|logout` | Thin pass-through to `sonar auth …` (login must be run by the user in a terminal). |
| `sqa doctor` | Check `sonar` installed and version matches, authenticated (`sonar auth status`), server ≥ 2025.1, auth source (keychain vs env). |
| `sqa findings` | Unified, ranked list of issues + hotspots. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit`, `--format`. |
| `sqa next` | The single highest-priority open finding (for one-at-a-time loops). |
| `sqa show <key>` | Agent-ready brief: rule, rationale, location, code context (± N lines), data-flow if available, fix guidance from the rule. |

### State-changing (require user approval — see §8)

| Command | Description |
|---------|-------------|
| `sqa mark <issue-key> fp\|accept\|reopen --comment "…" --approved-by-user` | Transition an issue and attach the comment. |
| `sqa hotspot <hotspot-key> safe\|fixed\|acknowledged --comment "…" --approved-by-user` | Review a hotspot (status REVIEWED + resolution). |

### Skill

| Command | Description |
|---------|-------------|
| `sqa skill print` | Print the embedded `SKILL.md`. |
| `sqa skill install [--target agents\|claude\|opencode\|cursor\|<path>] [--project]` | Write `sqa/SKILL.md`. Default target `agents` = `~/.agents/skills/sqa/` (read by Codex, Cursor, OpenCode, Pi and others). `claude` = `~/.claude/skills/sqa/` (Claude Code does not read `~/.agents/skills`). `--project` writes to `./.agents/skills/sqa/` instead. Never touches MCP config. |

### Output & exit codes

- `--format toon` (default when stdout is not a TTY; token-efficient), `json`, `md`, `table`.
- Exit `0` success / nothing above threshold; `1` findings above threshold (for `findings --fail-on`); `2` usage/config error; `3` auth/server error; `4` approval missing.

## 7. Prioritisation (security first)

Every finding is normalised to `{key, kind(issue|hotspot), quality, severity, rule, file, line, new_code, status, message}` and ranked:

1. **Tier 1 – Vulnerabilities**: issues with software quality `SECURITY` (MQR mode) / type `VULNERABILITY`.
2. **Tier 2 – Security Hotspots** with status `TO_REVIEW` (ordered by vulnerability probability HIGH → LOW).
3. **Tier 3 – Reliability**: `RELIABILITY` / `BUG`.
4. **Tier 4 – Maintainability**: `MAINTAINABILITY` / `CODE_SMELL`.

Within a tier: severity (BLOCKER > HIGH > MEDIUM > LOW > INFO; standard mode mapped equivalently) → new code before overall code → file path → line. Both MQR and Standard Experience severities are supported and mapped to one scale.

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
- Every change is appended to `~/.local/state/sqa/audit.jsonl` (timestamp, profile, key, action, comment, OS user). The SonarQube comment is prefixed with `[sqa]` for traceability on the server side.
- `--dry-run` shows what would change.

## 9. Embedded SKILL.md (outline)

- What `sqa` is; prerequisite: `sqa doctor` must pass.
- **Hard rules**: never use, install or configure a SonarQube MCP server; never run `sonar integrate`; never run `sonar auth login` (the user does that); never set, read or print tokens; always ask the user before any triage decision.
- Triage order (§7) and how to read `findings` / `show` output.
- The fix loop: pick approved finding → fix → run tests → (re-scan via CI or `sonar` analysis if available) → re-check with `sqa show <key>`.
- How to present a recommendation and ask for approval (template).
- Never mark something FP/accepted just to reduce counts; justify every decision in the comment.

## 10. Project layout

```
sonarqube-agent-cli/
├── cmd/sqa/main.go
├── internal/
│   ├── config/      # config file, project resolution
│   ├── sonarcli/    # pinned sonar install + invocation (list issues, api get/post, auth), output parsing
│   ├── findings/    # normalisation + ranking
│   ├── approval/    # guardrails + audit log
│   └── output/      # toon/json/md/table renderers
├── skill/SKILL.md   # embedded via go:embed
├── testdata/        # recorded API + sonar CLI responses
├── .goreleaser.yaml
└── DESIGN.md
```

Libraries: `cobra` (commands), `gopkg.in/yaml.v3`; no HTTP client, all server traffic via `sonar`. Tests run against recorded fixtures; an optional integration test runs against a real server when `SQA_IT_PROFILE` is set.

## 11. Open questions

1. Licence check: may we redistribute the `sonar` binary, or keep download-on-setup?
2. Which `sonar` CLI version to pin initially?
3. Skill install path for Grok and any internal harnesses (default `~/.agents/skills` covers Codex, Cursor, OpenCode, Pi; Claude Code needs `~/.claude/skills`).
6. Can `sonar` pick one of several saved server connections per command, or is one active connection per user the model?
4. Should `sqa` also trigger analysis (`sonar analyze`) or rely on CI scans only?
5. Name: keep `sqa` or choose another?
