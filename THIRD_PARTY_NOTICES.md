# Third-party notices

Release builds of `sqa` (`sqa_<version>_linux_amd64.tar.gz` and
`sqa_<version>_windows_amd64.zip`) contain the following third-party
software. `sqa` itself is MIT licensed (see `LICENSE`). Each archive embeds
only the official `sonar` binary for that platform.

## SonarQube CLI (`sonar`)

| | |
| --- | --- |
| Component | SonarQube CLI (`sonar`), the official command-line interface for SonarQube |
| Version | 1.12.0.17622 |
| Copyright | Copyright (C) SonarSource Sàrl |
| License | GNU Lesser General Public License v3.0 (LGPL-3.0), see `licenses/sonarqube-cli-LGPL-3.0.txt`; the LGPL incorporates the GNU GPL v3.0, see `licenses/sonarqube-cli-GPL-3.0.txt` |
| Source code | https://github.com/SonarSource/sonarqube-cli (this version: tag [`1.12.0.17622`](https://github.com/SonarSource/sonarqube-cli/tree/1.12.0.17622)) |
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

The binary is a self-contained executable built by SonarSource. Third-party
components that SonarSource compiled into it are distributed under their own
licenses as listed in the upstream repository.

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
and tag above.

## Not included: `sonar-secrets`

`sonar` can install a separate secrets scanner, `sonar-secrets`, which
SonarSource distributes under its own commercial licence. It is **not bundled**
with `sqa`, and `sqa` never downloads it: `sqa` runs `sonar` with its own
`SONAR_USER_HOME` and never runs the commands that install it (`sonar
integrate`, `sonar analyze secrets`, the git hooks).
