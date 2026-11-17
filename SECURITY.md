# Security Policy

## Supported versions

Only the latest released version of `sqa` receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue, discussion or pull request for security problems.

Report them privately through GitHub's private vulnerability reporting: open the
repository's **Security** tab and click **Report a vulnerability**
(https://github.com/dansailer/sqa/security/advisories/new). Only the maintainers
can see the report. Please include:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- the `sqa version` output (it includes the bundled `sonar` version and checksum) and OS/arch.

You will get an acknowledgement within 5 working days. Fixes are released as a new patch version and noted in the release notes.

## Security design

- `sqa` never reads, stores or prints SonarQube tokens. Authentication is delegated to the official `sonar` CLI (`sqa auth login` runs `sonar auth login`, or `SONARQUBE_CLI_*` env vars).
- **Token at rest.** By default `sonar` stores the token in `~/.config/sqa/credentials.json` (or `%APPDATA%\sqa\credentials.json` on Windows) as **plain-text JSON**, protected only by file permissions: mode `0600` in a `0700` directory on Unix, and on Windows a protected DACL granting only the current user SID full control (`SetNamedSecurityInfo`). This is the same model as `~/.terraform.d/credentials.tfrc.json` (`terraform login`) or `~/.kube/config` (`oc login`): anyone who can read files as your user, or an administrator, can read the token. `sqa` creates the file already private before `sonar` writes to it, re-applies that protection after every `sonar` run, and `sqa doctor` reports and fixes it. `sqa` handles only the path and permissions, never the content. Keep the file out of backups and dotfile repositories you share, use a token with the narrowest permissions your work needs, and revoke it on the server if the file may have leaked. If you prefer the OS keychain, set `auth.store: keychain` in `config.yaml`.
- **No telemetry by default.** Upstream `sonar` sends usage events to `events.sonardata.io` and error reports to Sentry (`*.ingest.us.sentry.io`) unless `DO_NOT_TRACK=1` or its `telemetry.enabled` setting is false; both channels check the same switch. `sqa` sets `DO_NOT_TRACK=1` for every `sonar` it runs, unless `DO_NOT_TRACK` is already set in your environment (any value, including `0`, is passed through) or `sonar.telemetry: true` is in `config.yaml`. Verified with the bundled 1.12.0.17622 behind a logging proxy: no connections to either host by default, both present when opted in.
- `sqa` runs `sonar` with its own `SONAR_USER_HOME` (`~/.local/share/sqa/sonar-home`, or `%LOCALAPPDATA%\sqa\sonar-home` on Windows), so `sonar` never downloads the commercially licensed `sonar-secrets` scanner on its behalf.
- Release binaries bundle the official SonarSource `sonar` CLI, unmodified (see `THIRD_PARTY_NOTICES.md`). It is verified **at build time** (the download from `binaries.sonarsource.com` must match the SHA-256 compiled into `sqa`, in CI and in the release workflow) and **at extraction** (the embedded bytes are hashed before writing, and the written file is hashed again before it is atomically renamed into `~/.local/share/sqa/bin/sonar-<version>`). On later runs the file is re-hashed whenever its size or modification time changes, and replaced if it no longer matches. On Windows the extracted name is `sonar-<version>.exe` under `%LOCALAPPDATA%\sqa\bin`.
- Without a bundled `sonar` (e.g. `go install` builds), `sqa setup` downloads the pinned official binary and verifies its SHA-256 against the same compiled-in checksum before installing.
- A weekly workflow reports newer `sonar` releases as issues; bumping the bundled version is always a reviewed code change.
- No MCP servers are installed or used.
- State-changing commands require explicit `--approved-by-user` and a comment, and are logged to a local audit file.
- Release archives ship with `checksums.txt`, an SPDX SBOM and GitHub build provenance attestations (`gh attestation verify <archive> --repo dansailer/sqa`).

Vulnerabilities in the bundled `sonar` CLI itself belong to SonarSource; see https://github.com/SonarSource/sonarqube-cli/security. If you are unsure which project an issue belongs to, report it here and we will forward the relevant part.
