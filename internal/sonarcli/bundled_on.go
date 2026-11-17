//go:build bundled_sonar && linux && amd64

package sonarcli

import _ "embed"

// bundledData is the official sonar binary for linux/amd64, fetched and
// checksum-verified by `make fetch-sonar` before the build. It is not
// committed to git.
//
//go:embed bundled/sonar-linux-amd64
var bundledData []byte

func init() {
	bundled = &Bundle{
		Version:  PinnedVersion,
		Platform: "linux-x86-64",
		SHA256:   pinnedChecksums["linux-x86-64"],
		Data:     bundledData,
	}
}
