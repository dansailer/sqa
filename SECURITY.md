# Security Policy

## Supported versions

Only the latest released version of `sqa` receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.
Report them privately to the maintainer via a GitHub direct contact or email listed on the maintainer's profile ([@dansailer](https://github.com/dansailer)), including:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- the `sqa version` output (it includes the bundled `sonar` version and checksum) and OS/arch.

You will get an acknowledgement within 5 working days. Fixes are released as a new patch version and noted in the release notes.

## Security design

- `sqa` never reads, stores or prints SonarQube tokens. Authentication is delegated to the official `sonar` CLI (OS keychain via `sonar auth login`, or `SONARQUBE_CLI_*` env vars).
- Release binaries bundle the official SonarSource `sonar` CLI, unmodified (see `THIRD_PARTY_NOTICES.md`). It is verified **at build time** (the download from `binaries.sonarsource.com` must match the SHA-256 compiled into `sqa`, in CI and in the release workflow) and **at extraction** (the embedded bytes are hashed before writing, and the written file is hashed again before it is atomically renamed into `~/.local/share/sqa/bin/sonar-<version>`). On later runs the file is re-hashed whenever its size or modification time changes, and replaced if it no longer matches.
- Without a bundled `sonar` (e.g. `go install` builds), `sqa setup` downloads the pinned official binary and verifies its SHA-256 against the same compiled-in checksum before installing.
- A weekly workflow reports newer `sonar` releases as issues; bumping the bundled version is always a reviewed code change.
- No MCP servers are installed or used.
- State-changing commands require explicit `--approved-by-user` and a comment, and are logged to a local audit file.
- Release archives ship with `checksums.txt`, an SPDX SBOM and GitHub build provenance attestations (`gh attestation verify <archive> --repo dansailer/sqa`).
