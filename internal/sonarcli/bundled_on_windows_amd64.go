//go:build bundled_sonar && windows && amd64

package sonarcli

import _ "embed"

// bundledData is the official sonar binary for windows/amd64, fetched and
// checksum-verified by `mise run fetch-sonar-windows` before the build. It
// is not committed to git.
//
//go:embed bundled/sonar-windows-amd64.exe
var bundledData []byte

func init() {
	bundled = &Bundle{
		Version:  PinnedVersion,
		Platform: "windows-x86-64",
		SHA256:   pinnedChecksums["windows-x86-64"],
		Data:     bundledData,
	}
}
