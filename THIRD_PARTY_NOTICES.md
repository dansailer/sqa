# Third-party notices

Release builds of `sqa` (`sqa_<version>_linux_amd64.tar.gz` and
`sqa_<version>_windows_amd64.zip`) contain the following third-party
software. `sqa` itself is MIT licensed (see `LICENSE`). Each archive embeds
only the official `sonar` binary for that platform.

`sqa` is not a SonarSource product and is not affiliated with or endorsed by
SonarSource; SonarQube and SonarSource are trademarks of SonarSource SA.

Contents of each release archive:

| Path | What |
| --- | --- |
| `LICENSE` | MIT licence of `sqa` |
| `THIRD_PARTY_NOTICES.md` | this file |
| `licenses/sonarqube-cli-LGPL-3.0.txt`, `licenses/sonarqube-cli-GPL-3.0.txt` | licence of the bundled `sonar` |
| `licenses/go/` | licence, notice and patent files of every Go module compiled into `sqa` and of Go itself (index: `licenses/go/MODULES.txt`) |

## SonarQube CLI (`sonar`)

| | |
| --- | --- |
| Component | SonarQube CLI (`sonar`), the official command-line interface for SonarQube |
| Version | 1.12.0.17622 |
| Copyright | Copyright (C) SonarSource Sàrl |
| License | GNU Lesser General Public License v3.0 (LGPL-3.0), see `licenses/sonarqube-cli-LGPL-3.0.txt`; the LGPL incorporates the GNU GPL v3.0, see `licenses/sonarqube-cli-GPL-3.0.txt` |
| Source code | https://github.com/SonarSource/sonarqube-cli (this version: tag [`1.12.0.17622`](https://github.com/SonarSource/sonarqube-cli/tree/1.12.0.17622)) |
| Source code (copy shipped with each `sqa` release) | `sonarqube-cli-1.12.0.17622-source.tar.gz`, a release asset next to the archives on the same GitHub release page; it is SonarSource's tag archive https://github.com/SonarSource/sonarqube-cli/archive/refs/tags/1.12.0.17622.tar.gz, downloaded by the release workflow and verified against the SHA-256 below |
| SHA-256 (source tarball) | `0d714bbefe108de876aa5a8af4b21c504a4b93e849c95a5eddb9c59d5648836d` |
| Binary redistributed (linux/amd64) | https://binaries.sonarsource.com/Distribution/sonarqube-cli/1.12.0.17622/linux/sonarqube-cli-1.12.0.17622-linux-x86-64.bin |
| SHA-256 (linux/amd64) | `c692e64dad89ea9e2388ec338dad47fa8703954e1c0ca8af111bc43e34afaa99` |
| Binary redistributed (windows/amd64) | https://binaries.sonarsource.com/Distribution/sonarqube-cli/1.12.0.17622/windows/sonarqube-cli-1.12.0.17622-windows-x86-64.exe |
| SHA-256 (windows/amd64) | `52dc02877bfc25bd436f7ff596821fd3605cfa5b760321866456a1d6590b984a` |

The binary is redistributed **unmodified**, exactly as published by
SonarSource at the URL for your platform above. It is stored as an opaque
file inside the `sqa` executable and, on first use, written unchanged to
`~/.local/share/sqa/bin/sonar-1.12.0.17622` (or under `$XDG_DATA_HOME`), or
to `%LOCALAPPDATA%\sqa\bin\sonar-1.12.0.17622.exe` on Windows, where its
SHA-256 is checked against the value above. `sqa` runs it as a separate
program; it is not linked into `sqa`. You can verify it yourself:

```sh
sha256sum ~/.local/share/sqa/bin/sonar-1.12.0.17622
# Windows: Get-FileHash "$env:LOCALAPPDATA\sqa\bin\sonar-1.12.0.17622.exe"
```

### What is inside the `sonar` binary

The binary is a self-contained executable that SonarSource builds with
`bun build --compile`: the Bun runtime with the CLI's bundled JavaScript
appended. Its components keep their own licences. The lists below are our
best reading of the upstream sources for this version; SonarSource does not
publish a bill of materials for the binary, so they may be incomplete.

**Bun runtime** v1.4.4 (the version in the upstream `mise.toml` and in the
binary's own version string), MIT licensed, https://github.com/oven-sh/bun/tree/bun-v1.4.4.
Bun's licence page for that version,
https://github.com/oven-sh/bun/blob/bun-v1.4.4/docs/project/license.mdx,
lists everything Bun links, in full. In short:

| Component in Bun | Licence |
| --- | --- |
| JavaScriptCore and WebCore files from WebKit (Bun's fork: https://github.com/oven-sh/WebKit), statically linked | LGPL-2.0/2.1 (some files BSD) |
| tinycc | LGPL-2.1 |
| boringssl | several licences (OpenSSL/ISC/MIT-style) |
| libicu 78 | ICU licence |
| simdutf, uSockets, a fork of uWebSockets, highway, parts of TigerBeetle's IO code | Apache-2.0 |
| zstd | BSD or GPL-2.0 |
| brotli, mimalloc, c-ares, ls-hpack, ls-qpack, lsquic, libdeflate, libuv (Windows) | MIT |
| picohttpparser | Perl licence or MIT |
| lol-html, libwebp, libjpeg-turbo | BSD-3-Clause (libjpeg-turbo also IJG/zlib) |
| libbase64, libspng, HdrHistogram_c | BSD-2-Clause (HdrHistogram_c also CC0) |
| zlib-ng | zlib |
| libarchive | several licences (BSD-style) |
| sqlite (Linux and Windows) | public domain |
| Node.js polyfills (`buffer`, `events`, `process`, `url`, `util`, …) and type declarations (`@types/node`, `undici-types`) | MIT |

The LGPL relinking instructions for JavaScriptCore are on Bun's licence page
above; the WebKit source is at https://github.com/oven-sh/WebKit.

**npm packages.** The production dependency tree of `sonarqube-cli`
1.12.0.17622, as resolved by its `bun.lock` (from `package.json`
`dependencies`, not `devDependencies`). `bun build --compile` bundles only
the modules that are actually imported, so some of these are probably not
in the binary, in particular `@sentry/bundler-plugins` and its build-time
dependencies (`oxc-parser` and its native bindings, `@rollup/*`, `glob`,
`magic-string`, `sentry`). The `<platform>` packages are native bindings for
the build platform.

| Package | Version | Licence |
| --- | --- | --- |
| `@clack/core` | 1.5.1 | MIT |
| `@jridgewell/sourcemap-codec` | 1.5.5 | MIT |
| `@opentelemetry/api` | 1.9.1 | Apache-2.0 |
| `@oxc-parser/binding-<platform>` | 0.152.0 | MIT |
| `@oxc-project/types` | 0.152.0 | MIT |
| `@rollup/rollup-<platform>` | 4.63.1 | MIT |
| `@sentry/bun` | 11.10.0 | MIT |
| `@sentry/bundler-plugins` | 11.10.0 | MIT |
| `@sentry/conventions` | 0.29.0 | MIT |
| `@sentry/core` | 11.10.0 | MIT |
| `@sentry/node` | 11.10.0 | MIT |
| `@sentry/opentelemetry` | 11.10.0 | MIT |
| `@sentry/server-runtime-injection` | 11.10.0 | MIT |
| `@sentry/server-utils` | 11.10.0 | MIT |
| `@toon-format/toon` | 2.3.1 | MIT |
| `argparse` | 3.0.3 | PSF-2.0 |
| `balanced-match` | 4.0.4 | MIT |
| `base64-js` | 1.5.1 | MIT |
| `brace-expansion` | 5.0.12 | MIT |
| `commander` | 15.0.0 | MIT |
| `dotenv` | 17.4.2 | BSD-2-Clause |
| `fast-deep-equal` | 2.0.1 | MIT |
| `fast-string-truncated-width` | 3.0.3 | MIT |
| `fast-string-width` | 3.0.2 | MIT |
| `fast-wrap-ansi` | 0.2.2 | MIT |
| `glob` | 13.0.6 | BlueOak-1.0.0 |
| `graceful-fs` | 4.2.11 | ISC |
| `has-flag` | 4.0.0 | MIT |
| `imurmurhash` | 0.1.4 | MIT |
| `js-yaml` | 5.5.0 | MIT |
| `launchdarkly-eventsource` | 2.0.3 | MIT |
| `launchdarkly-js-sdk-common` | 5.8.3 | Apache-2.0 |
| `launchdarkly-node-client-sdk` | 3.4.3 | Apache-2.0 |
| `lru-cache` | 11.5.3 | BlueOak-1.0.0 |
| `magic-string` | 0.30.21 | MIT |
| `minimatch` | 10.2.6 | BlueOak-1.0.0 |
| `minipass` | 7.1.3 | BlueOak-1.0.0 |
| `neverthrow` | 8.2.0 | MIT |
| `node-forge` | 1.4.3 | BSD-3-Clause OR GPL-2.0 |
| `node-localstorage` | 1.3.1 | MIT |
| `openpgp` | 6.3.2 | LGPL-3.0-or-later |
| `oxc-parser` | 0.152.0 | MIT |
| `path-scurry` | 2.0.2 | BlueOak-1.0.0 |
| `picocolors` | 1.1.1 | ISC |
| `picomatch` | 4.0.7 | MIT |
| `sentry` | 0.45.0 | FSL-1.1-Apache-2.0 |
| `sisteransi` | 1.0.5 | MIT |
| `slide` | 1.1.6 | ISC |
| `smol-toml` | 1.9.1 | BSD-3-Clause |
| `supports-color` | 8.1.1 | MIT |
| `write-file-atomic` | 1.3.4 | ISC |

The licence texts of these components are in their source repositories
(and, for npm packages, in each package tarball on https://www.npmjs.com).

### Replacing the bundled `sonar`

You may use any other build of the SonarQube CLI, including one you built from
the source above. Set `sonar.path` in `~/.config/sqa/config.yaml` (or
`%APPDATA%\sqa\config.yaml` on Windows):

```yaml
sonar:
  path: /path/to/your/sonar
```

`sonar.path` always takes precedence over the bundled binary. Alternatively,
`sonar.version: "<version>"` plus `sqa setup --sonar-version <version>`
downloads another official release from SonarSource and uses it instead.

The corresponding source code is published by SonarSource at the repository
and tag above, and a copy of it (`sonarqube-cli-1.12.0.17622-source.tar.gz`)
is attached to every `sqa` release that bundles this version.

## Not included: `sonar-secrets`

`sonar` can install a separate secrets scanner, `sonar-secrets`, which
SonarSource distributes under its own commercial licence. It is **not bundled**
with `sqa`, and `sqa` never downloads it: `sqa` runs `sonar` with its own
`SONAR_USER_HOME` and never runs the commands that install it (`sonar
integrate`, `sonar analyze secrets`, the git hooks).

## Go modules

`sqa` is built with Go and statically links the Go standard library and
runtime and these modules. Their licence, notice and patent files are in
`licenses/go/` in every release archive (generated by `mise run go-licenses`,
see `tools/go-licenses`).

| Module | Licence |
| --- | --- |
| Go standard library and runtime (`go`) | BSD-3-Clause, plus `PATENTS` |
| `github.com/spf13/cobra` | Apache-2.0 |
| `github.com/spf13/pflag` | BSD-3-Clause |
| `github.com/inconshreveable/mousetrap` (Windows builds only) | Apache-2.0 |
| `golang.org/x/sys` | BSD-3-Clause, plus `PATENTS` |
| `golang.org/x/term` | BSD-3-Clause, plus `PATENTS` |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0, plus `NOTICE` |
