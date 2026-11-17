package sonarcli

import (
	"os"
	"strings"
	"testing"
)

// TestThirdPartyNoticesMatchPinnedVersion keeps THIRD_PARTY_NOTICES.md in
// step with the bundled sonar: bumping PinnedVersion or its checksum without
// updating the notice fails here.
func TestThirdPartyNoticesMatchPinnedVersion(t *testing.T) {
	b, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	notice := string(b)
	in := &Installer{BaseURL: DistributionBaseURL}
	for _, want := range []string{
		"| Version | " + PinnedVersion + " |",
		"tree/" + PinnedVersion,
		in.ArtifactURL(PinnedVersion, "linux", "linux-x86-64"),
		PinnedChecksum("linux-x86-64"),
		"sonar-" + PinnedVersion,
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("THIRD_PARTY_NOTICES.md does not mention %q", want)
		}
	}
	for _, f := range []string{"../../licenses/sonarqube-cli-LGPL-3.0.txt", "../../licenses/sonarqube-cli-GPL-3.0.txt"} {
		if st, err := os.Stat(f); err != nil || st.Size() < 5000 {
			t.Errorf("%s missing or truncated: %v", f, err)
		}
	}
}
