package sonarcli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// trademarkNotice must appear in README.md and THIRD_PARTY_NOTICES.md.
const trademarkNotice = "`sqa` is not a SonarSource product and is not affiliated with or endorsed by SonarSource; SonarQube and SonarSource are trademarks of SonarSource SA."

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
		in.ArtifactURL(PinnedVersion, "windows", "windows-x86-64"),
		PinnedChecksum("windows-x86-64"),
		"sonar-" + PinnedVersion,
		"sonarqube-cli-" + PinnedVersion + "-source.tar.gz",
		"archive/refs/tags/" + PinnedVersion + ".tar.gz",
		sourceChecksum(t),
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

// sourceChecksum reads the pinned SHA-256 of the sonar source tarball that the
// release workflow attaches (mise run fetch-sonar-source).
func sourceChecksum(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^sonar_source_sha256 = "([0-9a-f]{64})"$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("mise.toml: sonar_source_sha256 not found")
	}
	return string(m[1])
}

func TestTrademarkNotice(t *testing.T) {
	for _, f := range []string{"../../README.md", "../../THIRD_PARTY_NOTICES.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// README quotes it; compare with blockquote markers and line breaks removed.
		text := strings.Join(strings.Fields(strings.ReplaceAll(string(b), "\n> ", "\n")), " ")
		if !strings.Contains(text, strings.Join(strings.Fields(trademarkNotice), " ")) {
			t.Errorf("%s lacks the trademark notice", f)
		}
	}
}
