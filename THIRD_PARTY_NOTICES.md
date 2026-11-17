# Third-party notices

Release builds of `sqa` (`sqa_<version>_linux_amd64.tar.gz`) contain the
following third-party software. `sqa` itself is MIT licensed (see `LICENSE`).

## SonarQube CLI (`sonar`)

| | |
| --- | --- |
| Component | SonarQube CLI (`sonar`), the official command-line interface for SonarQube |
| Version | 1.12.0.17622 |
| Copyright | Copyright (C) SonarSource Sàrl |
| License | GNU Lesser General Public License v3.0 (LGPL-3.0), see `licenses/sonarqube-cli-LGPL-3.0.txt`; the LGPL incorporates the GNU GPL v3.0, see `licenses/sonarqube-cli-GPL-3.0.txt` |
| Source code | https://github.com/SonarSource/sonarqube-cli (this version: tag [`1.12.0.17622`](https://github.com/SonarSource/sonarqube-cli/tree/1.12.0.17622)) |
| Binary redistributed | https://binaries.sonarsource.com/Distribution/sonarqube-cli/1.12.0.17622/linux/sonarqube-cli-1.12.0.17622-linux-x86-64.bin |
| SHA-256 | `c692e64dad89ea9e2388ec338dad47fa8703954e1c0ca8af111bc43e34afaa99` |

The binary is redistributed **unmodified**, exactly as published by
SonarSource at the URL above. It is stored as an opaque file inside the `sqa`
executable and, on first use, written unchanged to
`~/.local/share/sqa/bin/sonar-1.12.0.17622` (or under `$XDG_DATA_HOME`), where
its SHA-256 is checked against the value above. `sqa` runs it as a separate
program; it is not linked into `sqa`. You can verify it yourself:

```sh
sha256sum ~/.local/share/sqa/bin/sonar-1.12.0.17622
```

The binary is a self-contained executable built by SonarSource. Third-party
components that SonarSource compiled into it are distributed under their own
licenses as listed in the upstream repository.

### Replacing the bundled `sonar`

You may use any other build of the SonarQube CLI, including one you built from
the source above. Set `sonar.path` in `~/.config/sqa/config.yaml`:

```yaml
sonar:
  path: /path/to/your/sonar
```

`sonar.path` always takes precedence over the bundled binary. Alternatively,
`sonar.version: "<version>"` plus `sqa setup --sonar-version <version>`
downloads another official release from SonarSource and uses it instead.

To obtain the corresponding source code, use the repository and tag above. If
it is ever unavailable there, open an issue at
https://github.com/dansailer/sqa/issues and a copy will be provided.
