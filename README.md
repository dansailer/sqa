# sqa: SonarQube findings for coding agents

`sqa` lets coding agents (Claude Code, OpenCode, Codex, Cursor, Grok, …) read
findings from a self-hosted **SonarQube Server 2025.1+**, ranked
**security first**, and help fix them, while **you approve every triage
decision**.

- Wraps the official SonarSource [`sonar` CLI](https://github.com/SonarSource/sonarqube-cli),
  which release builds **bundle inside the `sqa` binary**: one download
  contains everything. All server traffic goes through it, so `sqa` has no HTTP client for SonarQube
  and never reads, stores or prints a token.
- **No MCP.** `sqa` never installs or calls a SonarQube MCP server and never runs
  `sonar integrate` or `sonar remediate`.
- Ships a harness-neutral `SKILL.md` that teaches agents the triage order and the
  human-in-the-loop rule.

## Install

Release binaries are currently built for **Linux amd64 only**.

Each release binary contains the official SonarSource `sonar` CLI
(`1.12.0.17622`, unmodified, LGPL-3.0, see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)). On first use `sqa` extracts
it to `~/.local/share/sqa/bin/sonar-<version>` and checks its SHA-256, so
there is nothing else to install and `sqa setup` is optional.

### With mise (recommended)

[mise](https://mise.jdx.dev) installs `sqa` straight from the GitHub releases,
verifies the download against `checksums.txt`, and installs the skill:

```sh
mise use -g --postinstall "sqa skill install" github:dansailer/sqa
sqa auth login       # you, once, in a terminal (browser login, token goes to the OS keychain)
```

mise saves the hook in `~/.config/mise/config.toml`, so `mise upgrade` reinstalls
the skill with every new version. Use
`--postinstall "sqa skill install && sqa skill install --target claude"` if you
also use Claude Code.

> While the repository is private, mise needs a GitHub token to see the
> releases: `export GITHUB_TOKEN=$(gh auth token)` (or `MISE_GITHUB_TOKEN`).

### Which `sonar` is used

1. `sonar.path` from the config file, if set.
2. The bundled `sonar` (release builds), extracted on first use. Skipped when
   `sonar.version` in the config names a different version.
3. The `sonar` installed by `sqa setup` (`~/.local/share/sqa/bin/sonar`).
4. `sonar` on your `PATH`.

`sqa version` and `sqa doctor` show whether `sonar` is bundled and which one is
in use. `sqa setup` with a bundled build just extracts and verifies it
(`--force` re-extracts); `sqa setup --sonar-version <v>` downloads another
official release, which `sqa` uses once you set `sonar.version: "<v>"`.

## Quickstart

```sh
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
| `sqa setup [--force] [--sonar-version V]` | Optional. Extract and verify the bundled `sonar`, or download another official version (checksum-verified). |
| `sqa doctor` | Check the `sonar` install and version, authentication (`sonar auth status`) and server version (2025.1 or newer). |
| `sqa auth login\|status\|logout` | Pass-through to `sonar auth …`. Run `login` yourself; agents must not. |
| `sqa findings` | Open issues and to-review hotspots, ranked security first. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`. |
| `sqa next` | The highest-priority finding (same filters). |
| `sqa show <key> [--context N] [--branch B\|--pr N]` | Agent-ready brief for an issue or hotspot. |
| `sqa mark <issue-key> fp\|accept\|reopen --comment "…" --approved-by-user [--dry-run]` | Transition an issue and add a `[sqa]` comment. |
| `sqa hotspot <key> safe\|fixed\|acknowledged --comment "…" --approved-by-user [--dry-run]` | Review a hotspot (status `REVIEWED`). |
| `sqa skill print` | Print the embedded `SKILL.md`. |
| `sqa skill install [--target agents\|claude\|opencode\|cursor\|<path>] [--project]` | Install the skill. |
| `sqa version` | Version of `sqa`, the pinned `sonar` CLI, whether it is bundled, and which `sonar` is in use. |

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
  version: "1.12.0.17622"      # sonar CLI version (default: bundled/pinned); another value uses `sqa setup --sonar-version` downloads
  path: ""                     # use this sonar binary instead of the bundled or downloaded one
skills:
  default_dir: ~/.agents/skills
```

## Security notes

- Tokens are handled only by `sonar`, which stores them in the OS keychain or
  reads `SONARQUBE_CLI_*` env vars. `sqa` never reads, logs or prints them.
- The bundled `sonar` is verified three times: at build time (`make
  fetch-sonar` checks the download against the SHA-256 compiled into `sqa`),
  before extraction (the embedded bytes), and after writing it to disk. Later
  runs re-hash it whenever its size or modification time changes, and replace
  it if it no longer matches.
- `sqa` itself makes no network requests, except `sqa setup` downloading a
  non-bundled `sonar` from `binaries.sonarsource.com`. The pinned version is
  checked against SHA-256 digests compiled into `sqa`. For any other version,
  the checksum is fetched from SonarSource's published `.sha256` file.
- `sqa` refuses to run `sonar integrate` (installs MCP servers and hooks) and
  `sonar remediate`.
- Release archives come with `checksums.txt`, an SPDX SBOM, plus GitHub build
  provenance attestations once the repository is public. Workflows use SHA-pinned actions, least-privilege
  tokens and `step-security/harden-runner`.

## Development

```sh
make test            # go test -race ./...
make lint            # go vet + golangci-lint
make fetch-sonar     # download + verify the pinned sonar (linux/amd64)
make test-bundled    # tests with -tags bundled_sonar
make build-bundled   # ./sqa with sonar embedded
```

Tests run against a fake `sonar` script and recorded SonarQube responses in
`testdata/fake/`. Releases are cut by pushing a `v*` tag. The release workflow
fetches and verifies `sonar`, builds linux/amd64 with `-tags bundled_sonar`
(`CGO_ENABLED=0`, `-trimpath`, version injected with `-ldflags`), packages it
as `tar.gz`, and publishes it with `gh release create`. A weekly workflow
(`sonar-update.yml`) checks SonarSource's `stable.version` and opens an issue
when a newer `sonar` is available.

See [DESIGN.md](DESIGN.md) for the design.

## License

`sqa` is MIT licensed. The bundled `sonar` CLI is SonarSource's, licensed under
LGPL-3.0 and redistributed unmodified; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for its source, checksum and
how to replace it with your own build (`sonar.path`).
