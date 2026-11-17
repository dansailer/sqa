# sqa: SonarQube findings for coding agents

`sqa` lets coding agents (Claude Code, OpenCode, Codex, Cursor, Grok, …) read
findings from a self-hosted **SonarQube Server 2025.1+**, ranked
**security first**, and help fix them, while **you approve every triage
decision**.

- Wraps the official SonarSource [`sonar` CLI](https://github.com/SonarSource/sonarqube-cli).
  All server traffic goes through it, so `sqa` has no HTTP client for SonarQube
  and never reads, stores or prints a token.
- **No MCP.** `sqa` never installs or calls a SonarQube MCP server and never runs
  `sonar integrate` or `sonar remediate`.
- Ships a harness-neutral `SKILL.md` that teaches agents the triage order and the
  human-in-the-loop rule.

## Install

### Release binaries

Download the archive for your platform from the
[releases page](https://github.com/dansailer/sqa/releases) (macOS and Linux,
amd64 and arm64), verify it, and put `sqa` on your `PATH`:

```sh
VERSION=0.1.0
OS=darwin    # or linux
ARCH=arm64   # or amd64
curl -fsSLO "https://github.com/dansailer/sqa/releases/download/v${VERSION}/sqa_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/dansailer/sqa/releases/download/v${VERSION}/checksums.txt"
shasum -a 256 --ignore-missing -c checksums.txt
gh attestation verify "sqa_${VERSION}_${OS}_${ARCH}.tar.gz" --repo dansailer/sqa   # build provenance
tar -xzf "sqa_${VERSION}_${OS}_${ARCH}.tar.gz"
install -m 0755 "sqa_${VERSION}_${OS}_${ARCH}/sqa" ~/.local/bin/sqa
```

### From source

```sh
go install github.com/dansailer/sqa/cmd/sqa@latest
```

### The official `sonar` CLI

```sh
sqa setup
```

downloads the pinned `sonar` release (currently `1.12.0.17622`) from
SonarSource's official distribution
(`https://binaries.sonarsource.com/Distribution/sonarqube-cli/`), verifies its
SHA-256 against the checksum embedded in `sqa`, and installs it to
`~/.local/share/sqa/bin/sonar`. To use another `sonar` binary, set
`sonar.path` in the config file. Otherwise `sqa` falls back to `sonar` on your
`PATH`.

> SonarSource publishes `sonar` for Linux x86-64/arm64 and macOS arm64 only.
> On Intel Macs, install `sonar` another way and set `sonar.path`.

## Quickstart

```sh
sqa setup                                              # install the pinned sonar CLI
sqa auth login --server https://sonarqube.example.com  # you, in a terminal (browser flow, token in the OS keychain)
sqa doctor                                             # sonar ok, authenticated, server >= 2025.1
sqa skill install                                      # teach your agents (~/.agents/skills/sqa/SKILL.md)

cd my-service
sqa findings --project my-service                      # ranked list
sqa next                                               # the single most important finding
sqa show AYx1z2                                        # rule, why, code context, how to fix
```

For CI or headless runs, skip `auth login` and set `SONARQUBE_CLI_SERVER` and
`SONARQUBE_CLI_TOKEN`. `sonar` reads them and they take precedence over the
keychain.

## Commands

| Command | What it does |
| --- | --- |
| `sqa setup [--force] [--sonar-version V]` | Install the pinned official `sonar` CLI, checksum-verified. |
| `sqa doctor` | Check the `sonar` install and version, authentication (`sonar auth status`) and server version (2025.1 or newer). |
| `sqa auth login\|status\|logout` | Pass-through to `sonar auth …`. Run `login` yourself; agents must not. |
| `sqa findings` | Open issues and to-review hotspots, ranked security first. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`. |
| `sqa next` | The highest-priority finding (same filters). |
| `sqa show <key> [--context N] [--branch B\|--pr N]` | Agent-ready brief for an issue or hotspot. |
| `sqa mark <issue-key> fp\|accept\|reopen --comment "…" --approved-by-user [--dry-run]` | Transition an issue and add a `[sqa]` comment. |
| `sqa hotspot <key> safe\|fixed\|acknowledged --comment "…" --approved-by-user [--dry-run]` | Review a hotspot (status `REVIEWED`). |
| `sqa skill print` | Print the embedded `SKILL.md`. |
| `sqa skill install [--target agents\|claude\|opencode\|cursor\|<path>] [--project]` | Install the skill. |
| `sqa version` | Version of `sqa` and the pinned `sonar` CLI. |

Every command takes `--format toon|json|md|table`. The default is `table`
when stdout is a terminal and [TOON](https://github.com/toon-format/toon) (compact,
token-efficient) when piped, which is what agents see.

### Ranking

1. Vulnerabilities (MQR software quality `SECURITY`, or type `VULNERABILITY`)
2. Security hotspots `TO_REVIEW`, by vulnerability probability HIGH > MEDIUM > LOW
3. Reliability (`RELIABILITY` / `BUG`)
4. Maintainability (`MAINTAINABILITY` / `CODE_SMELL`)

Within a tier: severity (BLOCKER > HIGH > MEDIUM > LOW > INFO; Standard
Experience severities map CRITICAL→HIGH, MAJOR→MEDIUM, MINOR→LOW), then new code
first, then file and line. `--min-severity` accepts either naming scheme.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success (or nothing at or above `--fail-on`). |
| 1 | `findings --fail-on` matched. |
| 2 | Usage or configuration error (including a missing `sonar`). |
| 3 | Authentication or server error. |
| 4 | Approval missing (`--approved-by-user` or `--comment`). |

## Skill install

```sh
sqa skill install                    # ~/.agents/skills/sqa/SKILL.md  (Codex, Cursor, OpenCode, Pi, …)
sqa skill install --target claude    # ~/.claude/skills/sqa/SKILL.md
sqa skill install --target opencode  # ~/.config/opencode/skills/sqa/SKILL.md
sqa skill install --target cursor    # ~/.cursor/skills/sqa/SKILL.md
sqa skill install --project          # ./.agents/skills/sqa/SKILL.md (this repository only)
sqa skill install --target ~/my/skills   # ~/my/skills/sqa/SKILL.md
```

The skill is embedded in the binary, so re-running `install` after an upgrade
keeps it in sync. It never touches MCP configuration.

## Human in the loop

The skill tells agents to present each finding with a recommended verdict and
reasoning, then **stop and ask you**. Only after you approve that specific
finding may the agent fix it or run `sqa mark` / `sqa hotspot` with
`--approved-by-user`. A general "go ahead" does not cover findings the agent has
not presented, and agents must never mark something as a false positive or
accepted just to reduce the count.

The CLI enforces what it can:

- `mark` and `hotspot` refuse to run without `--approved-by-user` and a non-empty
  `--comment` (exit 4).
- The server-side comment is prefixed with `[sqa]`.
- Every attempt, including dry runs and failures, is appended to
  `~/.local/state/sqa/audit.jsonl` (time, OS user, key, action, comment, result).

## Configuration

`~/.config/sqa/config.yaml` is optional and never holds secrets (override the
location with `SQA_CONFIG`; `XDG_*` directories are honoured):

```yaml
default_project: my-service    # used when --project is omitted (else sonar auto-detects)
sonar:
  version: "1.12.0.17622"      # sonar CLI version for `sqa setup` (default: pinned)
  path: ""                     # use this sonar binary instead of the managed one
skills:
  default_dir: ~/.agents/skills
```

## Security notes

- Tokens are handled only by `sonar`, which stores them in the OS keychain or
  reads `SONARQUBE_CLI_*` env vars. `sqa` never reads, logs or prints them.
- The only network access `sqa` does itself is `sqa setup` downloading the
  `sonar` binary from `binaries.sonarsource.com`. The pinned version is checked
  against SHA-256 digests compiled into `sqa`. For any other version, the
  checksum is fetched from SonarSource's published `.sha256` file.
- `sqa` refuses to run `sonar integrate` (installs MCP servers and hooks) and
  `sonar remediate`.
- Release archives come with `checksums.txt`, an SPDX SBOM and GitHub build
  provenance attestations. Workflows use SHA-pinned actions, least-privilege
  tokens and `step-security/harden-runner`.

## Development

```sh
go test -race ./...
go vet ./...
golangci-lint run
```

Tests run against a fake `sonar` script and recorded SonarQube responses in
`testdata/fake/`. Releases are cut by pushing a `v*` tag. The release workflow
builds the four targets with `go build` (`CGO_ENABLED=0`, `-trimpath`, version
injected with `-ldflags`), packages them as `tar.gz`, and publishes them with
`gh release create`.

See [DESIGN.md](DESIGN.md) for the design.

## License

MIT. The `sonar` CLI downloaded by `sqa setup` is SonarSource's and is licensed
under LGPL-3.0. `sqa` downloads it at setup time and does not redistribute it.
