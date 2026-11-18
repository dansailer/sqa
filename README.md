# sqa: SonarQube findings for coding agents

`sqa` lets coding agents (Claude Code, OpenCode, Codex, Cursor, Grok, …) read
findings from a self-hosted **SonarQube Server 2025.1+**, ranked
**security first**, and help fix them, while **you approve every triage
decision**.

- Wraps the official SonarSource [`sonar` CLI](https://github.com/SonarSource/sonarqube-cli),
  which release builds **bundle inside the `sqa` binary**: one download
  contains everything. All server traffic goes through it, so `sqa` has no HTTP client for SonarQube
  and never reads, stores or prints a token.
- Keeps your login in a private credentials file (`0600`, or a user-only ACL on Windows), like `terraform login` or
  `oc login`, so Linux needs no keychain (libsecret) setup.
- **No MCP.** `sqa` never installs or calls a SonarQube MCP server and never runs
  `sonar integrate` or `sonar remediate`.
- Ships a harness-neutral `SKILL.md` that teaches agents the triage order and the
  human-in-the-loop rule.

> `sqa` is not a SonarSource product and is not affiliated with or endorsed by
> SonarSource; SonarQube and SonarSource are trademarks of SonarSource SA.

## Why sqa and not just `sonar`?

`sonar` 1.12 can technically do all of this via `sonar list issues` and
`sonar api` (hotspots, rules, source, transitions). `sqa` exists because:

- **`sonar list issues` does not include security hotspots.** `sqa findings` /
  `sqa next` merge open issues and to-review hotspots into one list, ranked
  security first.
- **One call, not four or five.** `sqa next` and `sqa show` assemble the brief
  an agent needs (rule description, code context, taint flow, fix guidance)
  instead of raw API JSON, saving tokens and turns.
- **A guardrail on writes.** State changes go only through `sqa mark` and
  `sqa hotspot` with `--approved-by-user`. The skill forbids raw `sonar api`
  calls; giving an agent `sonar api` would let it POST anything.
- **One download.** Releases bundle a pinned, checksum-verified `sonar`, use a
  file token store (no libsecret/keychain on Linux), give `sonar` its own home
  so `sonar-secrets` is never downloaded, and turn telemetry off by default.
  `sqa doctor` checks the setup; `sqa skill install` teaches the agent.

## Install

Release binaries are built for **Linux amd64** and **Windows amd64**. Each
release contains the official SonarSource `sonar` CLI (`1.12.0.17622`,
unmodified, LGPL-3.0, see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).
On first use `sqa` extracts it to `~/.local/share/sqa/bin/sonar-<version>`
(or `%LOCALAPPDATA%\sqa\bin\sonar-<version>.exe` on Windows) and checks
its SHA-256.

### With mise (recommended)

[mise](https://mise.jdx.dev) installs `sqa` from GitHub releases, verifies
`checksums.txt`, and can install the skill:

```sh
mise use -g --postinstall "sqa skill install" github:dansailer/sqa
sqa auth login --server https://sonarqube.example.com  # you, once, in a terminal
```

mise saves the hook in `~/.config/mise/config.toml`, so `mise upgrade` reinstalls
the skill with every new version. Use
`--postinstall "sqa skill install && sqa skill install --target claude"` if you
also use Claude Code.

### With curl | sh (Linux)

```sh
curl -fsSL https://raw.githubusercontent.com/dansailer/sqa/main/install.sh | sh
# pin a release:  ... | sh -s -- --version v0.2.0
```

Installs to `$SQA_INSTALL_DIR` (default `~/.local/bin`), verifies the archive
against `checksums.txt`, and runs `sqa skill install` (skip with `--no-skill`).

### With PowerShell (Windows)

```powershell
irm https://raw.githubusercontent.com/dansailer/sqa/main/install.ps1 | iex
# pin a release:  $env:SQA_VERSION = 'v0.3.0'; irm ... | iex
```

Installs to `$env:SQA_INSTALL_DIR` (default `%LOCALAPPDATA%\Programs\sqa`),
verifies the archive against `checksums.txt`, and runs `sqa skill install`
(skip with `$env:SQA_NO_SKILL = '1'`).

mise also works on Windows. While the repository is private, every method
needs a GitHub token to see the releases: `export GITHUB_TOKEN=$(gh auth token)`
(or `$env:GITHUB_TOKEN = (gh auth token)`), or an authenticated `gh`; mise
also accepts `MISE_GITHUB_TOKEN`.

### Which `sonar` is used

1. `sonar.path` from the config file, if set.
2. The bundled `sonar` (release builds), extracted on first use. Skipped when
   `sonar.version` in the config names a different version.
3. The `sonar` installed by `sqa setup` (`~/.local/share/sqa/bin/sonar`, or
   `%LOCALAPPDATA%\sqa\bin\sonar.exe` on Windows).
4. `sonar` on your `PATH`.

`sqa version` and `sqa doctor` show whether `sonar` is bundled and which one is
in use. `sqa setup` with a bundled build just extracts and verifies it
(`--force` re-extracts); `sqa setup --sonar-version <v>` downloads another
official release, which `sqa` uses once you set `sonar.version: "<v>"`.

## Quickstart

```sh
sqa auth login --server https://sonarqube.example.com  # you, in a terminal (browser flow)
sqa doctor                                             # sonar ok, authenticated, server >= 2025.1
sqa skill install                                      # teach your agents (~/.agents/skills/sqa/SKILL.md)

cd my-service
sqa findings --project my-service                      # ranked list
sqa next                                               # highest-priority finding with full brief
sqa show AYx1z2                                        # brief for a specific key
```

## Authentication

`sqa auth login` runs the official `sonar auth login` (browser flow, or
`--with-token` to paste a token on stdin). `sonar` writes the token; `sqa`
only tells it where and never opens the file itself.

- **File store (default).** The token is kept in
  `~/.config/sqa/credentials.json` (`$XDG_CONFIG_HOME/sqa/`, or
  `%APPDATA%\sqa\credentials.json` on Windows), plain-text JSON with
  mode `0600` in a `0700` directory (on Windows, a DACL granting only your
  user account full control), the same model as
  `~/.terraform.d/credentials.tfrc.json` or `~/.kube/config`. `sqa` creates the
  file with `0600` before `sonar` first writes to it and re-checks the mode
  after every `sonar` run; `sqa doctor` reports and fixes wrong permissions.
  Override the path with `auth.credentials_file`.
- **OS keychain (opt-in).** Set `auth.store: keychain` to use macOS Keychain,
  Windows Credential Manager or the Linux Secret Service. On Linux that needs
  libsecret and a running keyring, e.g. on Debian/Ubuntu
  `sudo apt install libsecret-1-0 gnome-keyring dbus-user-session`, then log
  out and back in. Over SSH or WSL, start a session with
  `dbus-run-session -- bash` and run
  `gnome-keyring-daemon --unlock --components=secrets` first.
- **Environment variables (CI, headless).** Skip `auth login` and set
  `SONARQUBE_CLI_SERVER` and `SONARQUBE_CLI_TOKEN`. `sonar` reads them and they
  take precedence over any stored login.

`sqa` runs `sonar` with its own home directory
(`~/.local/share/sqa/sonar-home`, or `%LOCALAPPDATA%\sqa\sonar-home` on
Windows; override with `sonar.home`) instead of
`~/.sonar`, so a login made with a standalone `sonar` is not picked up: run
`sqa auth login` once. This also keeps `sonar` from downloading the
`sonar-secrets` scanner (see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).
If you set `SONARQUBE_CLI_KEYCHAIN_FILE` or `SONAR_USER_HOME` yourself, `sqa`
uses your values. `sqa auth status` and `sqa doctor` show the token store and
`sonar` home in use.

## Commands

| Command | What it does |
| --- | --- |
| `sqa setup [--force] [--sonar-version V]` | Optional. Extract and verify the bundled `sonar`, or download another official version (checksum-verified). |
| `sqa doctor` | Check the token store (path, permissions), `sonar` home, the `sonar` install and version, authentication (`sonar auth status`) and server version (2025.1 or newer). |
| `sqa auth login\|status\|logout` | Pass-through to `sonar auth …`. Run `login` yourself; agents must not. |
| `sqa findings` | Open issues and to-review hotspots, ranked security first, with a `PR` column for findings that already have an open fix PR. Flags: `--project`, `--branch`, `--pr`, `--file`, `--new-code`, `--min-severity`, `--security-only`, `--limit` (default 50, 0 = all), `--fail-on`, `--no-pr-check`. |
| `sqa next` | Highest-priority finding **without an open fix PR**, with the same brief as `sqa show` (same filters). `--skip <key>` (repeatable) passes over a finding; `--include-open-pr` stops skipping findings with an open PR. |
| `sqa show <key> [--context N] [--branch B\|--pr N] [--no-pr-check]` | Agent-ready brief for an issue or hotspot, including any open fix PR. |
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

### Open fix PRs

`findings`, `next` and `show` check once per run whether a finding already has
an open fix PR, so an agent does not work on (or duplicate) a fix that is in
review. They ask your forge, not SonarQube, and `sqa` never handles a token:

1. `gh pr list --state open` (uses `gh`'s own login). A PR counts when its
   body contains the finding key (the skill links the finding there) or its
   head branch ends with the key, e.g. `sonar/go-s3649-azx1...`.
2. Without `gh` (or with a non-GitHub remote):
   `git ls-remote --heads origin 'sonar/*'`, matched by branch name. This
   finds the branch, not the PR, so the PR column shows `branch`.
3. If neither works, nothing is skipped and the output carries a `note`.

`pr_check` in the output says which one was used (`gh`, `git`, `unavailable`,
`off`). `--no-pr-check` skips the lookup, e.g. for `findings --fail-on` in CI.

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

## Security hotspots: `sonar` vs `sqa`

A security hotspot is security-sensitive code that someone has to **review**
before deciding whether it needs a fix. A vulnerability needs a fix. In
SonarQube a hotspot starts as `TO_REVIEW`. A review sets it to `REVIEWED` with
the resolution `FIXED`, `SAFE` or `ACKNOWLEDGED` (reviewed, fix still pending).

**What `sonar` 1.12 does with hotspots** (checked in the 1.12.0.17622 source
and `--help`):

- `sonar list` has only `issues`, `projects` and `orgs`. `sonar list issues`
  calls `/api/issues/search`, and SonarQube serves hotspots only from
  `/api/hotspots/*`, so they never show up there.
- `sonar quality-gate status` reports failing gate conditions and lists the
  files or issues behind them. Its security breakdown covers vulnerabilities
  only (the source says "hotspots are out of scope"). A failing "Security
  Hotspots Reviewed" condition is reported, but without the hotspots behind it.
- There is no command to show or review a hotspot. The only way is raw
  `sonar api get /api/hotspots/search`, `/api/hotspots/show` and
  `sonar api post /api/hotspots/change_status`. That returns raw JSON, and an
  agent that may run `sonar api` can also POST any other write.

**What `sqa` adds:**

- **Hotspots in the same queue as issues.** `sqa findings` and `sqa next` fetch
  `TO_REVIEW` hotspots (`/api/hotspots/search`, all pages) and rank them right
  after vulnerabilities, by review priority (vulnerability probability
  HIGH > MEDIUM > LOW), then new code first. `--security-only` shows only
  vulnerabilities and hotspots; `--file`, `--branch`, `--pr` and `--new-code`
  apply to hotspots too.
- **A review brief.** `sqa show <hotspot-key>` (and `sqa next`) combine
  `/api/hotspots/show`, the rule's description sections (what's the risk, how
  to assess it, how to fix it) and the flagged lines with surrounding code from
  `/api/sources/lines`. They add a link to the hotspot's page in SonarQube and
  the possible verdicts: fix (then mark fixed), safe, acknowledged.
- **A guarded review command.** `sqa hotspot <key> safe|fixed|acknowledged
  --comment "…" --approved-by-user` sets `REVIEWED` with that resolution and
  the comment (prefixed `[sqa]`) in one call. It refuses to run without the
  approval flag and a comment, supports `--dry-run`, and logs every attempt to
  the audit log. The skill tells agents to present each hotspot with a
  recommendation and wait for your decision. An agent never needs `sonar api`.

**Not covered yet:**

- Only `TO_REVIEW` hotspots are listed. Once a hotspot is `ACKNOWLEDGED` (fix
  pending) it leaves the queue, and `sqa` cannot list reviewed hotspots.
- `sqa` cannot set a hotspot back to `TO_REVIEW`; undo a wrong review in the
  SonarQube UI.
- No assigning and no comment-only changes.
- `sqa` does not show the quality gate, so it does not report the "Security
  Hotspots Reviewed" condition. Use `sonar quality-gate status` for that.

## Configuration

`~/.config/sqa/config.yaml` is optional and never holds secrets (override the
location with `SQA_CONFIG`; `XDG_*` directories are honoured). The token lives
in `credentials.json` next to it, written by `sonar`:

```yaml
default_project: my-service    # used when --project is omitted (else sonar auto-detects)
auth:
  store: file                  # file (default) or keychain
  credentials_file: ""         # default ~/.config/sqa/credentials.json (%APPDATA%\sqa\ on Windows)
sonar:
  version: "1.12.0.17622"      # sonar CLI version (default: bundled/pinned); another value uses `sqa setup --sonar-version` downloads
  path: ""                     # use this sonar binary instead of the bundled or downloaded one
  home: ""                     # SONAR_USER_HOME for sonar, default ~/.local/share/sqa/sonar-home
  telemetry: false             # true lets sonar send usage telemetry and Sentry error reports
skills:
  default_dir: ~/.agents/skills
```

## Security notes

- Tokens are handled only by `sonar`, which stores them in sqa's private
  credentials file (or the OS keychain with `auth.store: keychain`) or reads
  `SONARQUBE_CLI_*` env vars. `sqa` only manages the file's path and
  permissions and never reads, logs or prints a token.
- The bundled `sonar` is verified three times: at build time (`mise run
  fetch-sonar` checks the download against the SHA-256 compiled into `sqa`),
  before extraction (the embedded bytes), and after writing it to disk. Later
  runs re-hash it whenever its size or modification time changes, and replace
  it if it no longer matches.
- `sonar`'s usage telemetry (`events.sonardata.io`) and Sentry error reports
  are **off**: `sqa` runs it with `DO_NOT_TRACK=1` unless you set
  `DO_NOT_TRACK` yourself (any value is respected) or opt in with
  `sonar.telemetry: true`. `sqa doctor` and `sqa version` show the state.
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

Tools and tasks are managed with [mise](https://mise.jdx.dev): `mise.toml`
pins Go and golangci-lint (`mise.lock` records their checksums), and CI runs
the same tasks.

```sh
mise install                # Go + golangci-lint at the pinned versions
mise tasks                  # list tasks
mise run test               # go test -race ./...
mise run lint               # go vet + golangci-lint
mise run vulncheck          # govulncheck
mise run fetch-sonar        # download + verify the pinned sonar (linux/amd64)
mise run test-bundled       # vet + tests with -tags bundled_sonar
mise run build-bundled      # ./sqa with sonar embedded (VERSION=… COMMIT=… optional)
mise run go-licenses        # Go module licence files -> build/licenses/go
mise run fetch-sonar-source # pinned sonar source tarball -> dist/ (verified)
```

Tests run against a fake `sonar` script and recorded SonarQube responses in
`testdata/fake/`. Releases are cut by pushing a `v*` tag. The release workflow
fetches and verifies `sonar`, builds linux/amd64 with `-tags bundled_sonar`
(`CGO_ENABLED=0`, `-trimpath`, version injected with `-ldflags`) and
windows/amd64 the same way, packages them as `tar.gz` and `zip` with the
licence files, attaches the verified `sonar` source tarball, and publishes
everything with `gh release create`. A weekly workflow
(`sonar-update.yml`) checks SonarSource's `stable.version` and opens an issue
when a newer `sonar` is available.

See [DESIGN.md](DESIGN.md) for the design.

## License

`sqa` is MIT licensed. The bundled `sonar` CLI is SonarSource's, licensed under
LGPL-3.0 and redistributed unmodified. Each release attaches the matching
`sonar` source (`sonarqube-cli-<version>-source.tar.gz`), and each archive
carries the licence texts of `sonar` and of the Go modules in `sqa`
(`licenses/`). See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for the
components inside `sonar` (Bun, JavaScriptCore, npm packages), checksums and
how to replace it with your own build (`sonar.path`).

`sqa` is not a SonarSource product and is not affiliated with or endorsed by
SonarSource; SonarQube and SonarSource are trademarks of SonarSource SA.
