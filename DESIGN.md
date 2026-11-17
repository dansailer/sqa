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

- `sqa` pins a tested `sonar` version: **1.12.0.17622** (`sonarcli.PinnedVersion`), with the SHA-256 SonarSource publishes for each platform binary **compiled into `sqa`** (`pinnedChecksums`).
- Official source (the same as SonarSource's own `install.sh`): `https://binaries.sonarsource.com/Distribution/sonarqube-cli/<version>/<os>/sonarqube-cli-<version>-<platform>.bin`, checksum at `<binary>.sha256`. The GitHub releases of `SonarSource/sonarqube-cli` carry no binary assets.
- **Release builds embed it.** `mise run fetch-sonar` (`tools/fetch-sonar`, reusing `sonarcli.Installer`) downloads the linux-x86-64 binary to `internal/sonarcli/bundled/sonar-linux-amd64` (gitignored, never committed) and verifies it against the compiled-in checksum. `mise run build-bundled` builds with `-tags bundled_sonar`, which compiles `bundled_on.go` (`//go:embed bundled/sonar-linux-amd64`). The tag is only valid for linux/amd64: on any other target `bundled_unsupported.go` breaks the build on purpose. Without the tag (`go install`, plain `go build`), nothing is embedded and `sqa` behaves as before.
- **Extraction.** The OS can only exec a file, so on first use `sqa` writes the embedded bytes to `~/.local/share/sqa/bin/sonar-<version>` (`XDG_DATA_HOME` honoured): hash the embedded bytes, write to a temp file in the same directory, fsync, hash the written file, `chmod 0755`, atomic rename. A stamp file `sonar-<version>.verified` records digest, size and mtime; later runs only stat the file and compare (cheap), and re-hash when size or mtime changed, re-extracting on mismatch. Binaries left by other versions (`sonar-<other>`) are removed. The versioned file name means an upgrade never overwrites a binary another `sqa` process is running.
- **Resolution order:** `sonar.path` from config → bundled (extracted) → `sqa setup` download (`~/.local/share/sqa/bin/sonar`) → `sonar` on `PATH`. Setting `sonar.version` in config to a version other than the bundled one skips the bundle.
- **`sqa setup`** is optional with a bundled build: it performs (or with `--force` redoes) the extraction, runs `sonar --version` and reports. `sqa setup --sonar-version <v>` with another version downloads that official release (pinned checksum, or the published `.sha256` for other versions) to `~/.local/share/sqa/bin/sonar`, as for non-bundled builds. SonarSource publishes downloads for linux-x86-64, linux-arm64 and macos-arm64 (no macOS x86-64).
- **`sqa version` / `sqa doctor`** report whether `sonar` is bundled, its version and SHA-256, and which source is in use (`sonar.path`, `bundled`, `sqa setup`, `PATH`). `version` never writes anything; `doctor` extracts if needed.
- **Updates.** A new `sonar` means a new `sqa` release. `.github/workflows/sonar-update.yml` runs weekly (and on demand), reads `stable.version` from the distribution URL above (what SonarSource's `install.sh` uses to resolve the latest release), and opens an issue with the new version, binary URL and published checksum when it is newer than `PinnedVersion`. It never commits code or binaries; bumping is a reviewed change to `install.go` and `THIRD_PARTY_NOTICES.md` (a test keeps the two in step).
- `sqa` always invokes this binary (or `sonar.path`) and inherits whatever authentication `sonar` has: the keychain login from `sonar auth login`, or the `SONARQUBE_CLI_*` env vars, which take precedence. `sqa` never reads, stores or prints a token, and never runs `sonar integrate`.
- **Licence.** `sonar` is LGPL-3.0 (Copyright SonarSource Sàrl). It is redistributed unmodified as a separate executable that `sqa` runs as a child process (not linked). Release archives include `THIRD_PARTY_NOTICES.md` (component, version, copyright, upstream source tag, exact binary URL and SHA-256, how to replace it via `sonar.path`) and `licenses/sonarqube-cli-LGPL-3.0.txt` plus the GPL-3.0 text it incorporates.
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
| `sqa setup` | Optional: extract/verify the bundled `sonar`, or download another official version. |
| `sqa auth login\|status\|logout` | Thin pass-through to `sonar auth …` (login must be run by the user in a terminal). |
| `sqa doctor` | Check `sonar` installed and version matches (warn on mismatch), authenticated (`sonar auth status`, shows server and source), server ≥ 2025.1 (or Community Build ≥ 25.1). |
| `sqa findings` | Unified, ranked list of open issues (`OPEN`, `CONFIRMED`) + `TO_REVIEW` hotspots. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`, `--format`. |
| `sqa next` | The single highest-priority open finding (for one-at-a-time loops). |
| `sqa show <key>` | Agent-ready brief: rule, rationale (rule description sections `root_cause` / `assess_the_problem` / `how_to_fix`, or hotspot risk text), location, code context (`--context N`, default 5, HTML stripped), data-flow, possible verdicts, next step. Tries the key as an issue, then as a hotspot. |
| `sqa version` | sqa version, pinned `sonar` version, whether it is bundled (version, SHA-256) and which `sonar` is in use. |

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
│   ├── sonarcli/    # pinned sonar: bundle/extract, download, resolve, invocation, output parsing
│   │   └── bundled/ # sonar-linux-amd64 fetched at build time (gitignored)
│   ├── findings/    # normalisation + ranking
│   ├── approval/    # guardrails + audit log
│   └── output/      # toon/json/md/table renderers
├── skill/SKILL.md   # embedded via go:embed
├── tools/fetch-sonar/ # download + verify the pinned sonar for bundling
├── licenses/        # LGPL-3.0 / GPL-3.0 texts for the bundled sonar
├── THIRD_PARTY_NOTICES.md
├── mise.toml        # pinned Go + golangci-lint, tasks (build, test, lint, fetch-sonar, build-bundled, …)
├── mise.lock        # resolved tool versions + checksums
├── testdata/fake/   # fake `sonar` script + recorded API responses
├── .github/         # CI, CodeQL, release, sonar-update workflows, dependabot
└── DESIGN.md
```

Libraries: `cobra` (commands), `gopkg.in/yaml.v3`, `golang.org/x/term`; no HTTP client for SonarQube, all server traffic via `sonar` (`net/http` is used only by `sqa setup` and `tools/fetch-sonar` to download the `sonar` binary). Tests run a fake `sonar` shell script (`testdata/fake/sonar`) that serves recorded responses and logs every invocation, so they assert on the exact `sonar` arguments (e.g. that no `api post` happens without approval).

## 10a. Build, CI and release

- **Tools and tasks**: `mise.toml` pins Go and golangci-lint (`mise.lock` records versions, download URLs and checksums) and defines the tasks (`build`, `build-bundled`, `fetch-sonar`, `sonar-version`, `test`, `test-bundled`, `lint`, `vulncheck`, `clean`). Every workflow sets up tools with `jdx/mise-action` (pinned mise version, binary verified against mise's signed checksums, `mise install --locked`, tool installs cached) and runs the same `mise run …` tasks as a developer. Go module and build caches (`~/go/pkg/mod`, `~/.cache/go-build`, plus `~/.cache/golangci-lint` for lint) are kept with `actions/cache`, keyed on workflow, job, `go.sum` and `mise.lock`.
- **CI** (`.github/workflows/ci.yml`, push/PR to `main`): `mise run lint` (`go vet` + golangci-lint), `mise run test` with coverage, an untagged linux/amd64 `mise run build`, and a **bundled** job: restore the `sonar` binary from `actions/cache` (key: pinned version from `mise run sonar-version`), `mise run test-bundled` (runs `fetch-sonar` first, which downloads on a miss and re-verifies the SHA-256 either way; then `go vet` and `go test -race` with `-tags bundled_sonar`, including a test that the embedded bytes match the pinned checksum and that the extracted `sonar --version` runs), `mise run build-bundled`, smoke test (`sqa version`, `sqa setup`, `sqa doctor` must fail only on auth). `mise run vulncheck` (govulncheck).
- **CodeQL** (`codeql.yml`): Go, on push/PR and weekly; Go comes from mise, manual build mode with `go build ./...`.
- **Release** (`release.yml`, on tag `v*`): `mise run test`, then one linux/amd64 job: fresh `mise run fetch-sonar` (no sonar cache), embedded-binary test, `mise run build-bundled` with `VERSION=<tag> COMMIT=<sha>` (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X …/internal/cli.version=<tag> -X …/internal/cli.commit=<sha>"`, `-tags bundled_sonar`), smoke test, and packaging of `sqa_<version>_linux_amd64.tar.gz` (single root dir: `sqa`, `LICENSE`, `README.md`, `THIRD_PARTY_NOTICES.md`, `licenses/`; reproducible tar flags). A final job generates an SPDX SBOM, writes `checksums.txt` (`sha256sum`), creates build provenance attestations (`actions/attest-build-provenance`, skipped while the repo is private) for the archive and checksums, and publishes with `gh release create --generate-notes`. The asset name contains `linux` and `amd64`, which is what mise's `github:` backend matches. No goreleaser.
- **sonar update check** (`sonar-update.yml`): weekly + manual, `contents: read` at the top, `issues: write` only on its single job; opens an issue when `stable.version` is newer than the pinned version (see §4.1).
- **Hardening**: every action pinned to a full commit SHA with a version comment, `permissions: contents: read` at the top, `contents: write` / `id-token: write` / `attestations: write` only on the release job, `persist-credentials: false`, `step-security/harden-runner` (egress audit), timeouts, concurrency groups. Dependabot updates Go modules and actions weekly; the mise version in the workflows and the tools in `mise.toml` are bumped by hand (`mise lock --bump`).

## 11. Decisions and open questions

Resolved:

1. Licence and distribution: `sonar` (LGPL-3.0) is bundled unmodified inside the release binary, with `THIRD_PARTY_NOTICES.md` and the licence texts in every archive (§4.1). `sqa setup` remains for non-bundled builds and version overrides.
2. Pinned `sonar` version: 1.12.0.17622 (stable on 2026-11-17).
3. Multiple servers: not needed (one internal instance).
4. Name: `sqa`.
5. Platforms: release builds are Linux amd64 only for now (macOS/arm64 temporarily not provided).

Open:

1. Skill install path for Grok and any internal harnesses (default `~/.agents/skills` covers Codex, Cursor, OpenCode, Pi; `--target <path>` covers the rest).
2. Should `sqa` also trigger analysis (`sonar analyze`) or rely on CI scans only? (Currently CI only.)
3. JSON shapes were built from the `sonar` CLI source and documentation and exercised against the real `sonar` 1.12.0 binary with a stub server; they still need a run against the internal SonarQube instance.
