# Security Policy

## Supported versions

Only the latest released version of `sqa` receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.
Report them privately to the maintainer via a GitHub direct contact or email listed on the maintainer's profile ([@dansailer](https://github.com/dansailer)), including:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- the `sqa version` output and OS/arch.

You will get an acknowledgement within 5 working days. Fixes are released as a new patch version and noted in the release notes.

## Security design

- `sqa` never reads, stores or prints SonarQube tokens. Authentication is delegated to the official `sonar` CLI (OS keychain via `sonar auth login`, or `SONARQUBE_CLI_*` env vars).
- `sqa setup` downloads the pinned official `sonar` binary from `binaries.sonarsource.com` and verifies its SHA-256 checksum before installing.
- No MCP servers are installed or used.
- State-changing commands require explicit `--approved-by-user` and a comment, and are logged to a local audit file.
- Release archives ship with `checksums.txt`, an SPDX SBOM and GitHub build provenance attestations (`gh attestation verify <archive> --repo dansailer/sqa`).
