//go:build bundled_sonar && linux && amd64

package sonarcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestBundledBinary runs only in bundled builds: the embedded sonar must match
// the pinned checksum, extract, and report the pinned version.
func TestBundledBinary(t *testing.T) {
	b := Bundled()
	if b == nil {
		t.Fatal("bundled_sonar build without embedded sonar")
	}
	sum := sha256.Sum256(b.Data)
	if got := hex.EncodeToString(sum[:]); got != PinnedChecksum("linux-x86-64") {
		t.Fatalf("embedded sonar sha256 %s does not match pinned %s", got, PinnedChecksum("linux-x86-64"))
	}
	res, err := b.Extract(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	v, err := (&Runner{Bin: res.Path}).Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != ShortVersion(PinnedVersion) {
		t.Fatalf("sonar --version = %q, want %s", v, ShortVersion(PinnedVersion))
	}
}
