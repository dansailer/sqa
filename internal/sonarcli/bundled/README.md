`mise run fetch-sonar` downloads the pinned official SonarSource `sonar` binary
for linux/amd64 into this directory and verifies its SHA-256 against the
checksum compiled into sqa. `mise run build-bundled` then embeds it with the
`bundled_sonar` build tag. The binary is gitignored and never committed.
