package findings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileGlob(t *testing.T) {
	cases := []struct {
		pattern string
		yes     []string
		no      []string
	}{
		{"test/**", []string{"test/a.go", "test/x/y/z.go"}, []string{"src/test/a.go", "testdata/a.go"}},
		{"**/*_test.go", []string{"a_test.go", "internal/cli/cli_test.go"}, []string{"internal/cli/cli.go", "a_test.go.bak"}},
		{"internal/*/fixtures/*.json", []string{"internal/cli/fixtures/a.json"}, []string{"internal/cli/x/fixtures/a.json", "internal/cli/fixtures/a/b.json"}},
		{"src/file?.go", []string{"src/file1.go"}, []string{"src/file10.go", "src/file/.go"}},
		{"[!a]*.go", []string{"b.go"}, []string{"a.go"}},
		{"./docs/**/*.md", []string{"docs/a.md", "docs/x/b.md"}, []string{"a.md"}},
	}
	for _, c := range cases {
		m, err := CompileGlob(c.pattern)
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		for _, p := range c.yes {
			if !m(p) {
				t.Errorf("%s should match %s", c.pattern, p)
			}
		}
		for _, p := range c.no {
			if m(p) {
				t.Errorf("%s should not match %s", c.pattern, p)
			}
		}
	}
	if !IsGlob("test/**") || IsGlob("internal/api/users.go") {
		t.Error("IsGlob")
	}
}

func TestSeverityJSONIsTheName(t *testing.T) {
	b, err := json.Marshal(Finding{Key: "k", Severity: SevHigh})
	if err != nil || !strings.Contains(string(b), `"severity":"HIGH"`) {
		t.Fatalf("%s %v", b, err)
	}
	var f Finding
	if err := json.Unmarshal(b, &f); err != nil || f.Severity != SevHigh {
		t.Fatalf("%+v %v", f, err)
	}
	if err := json.Unmarshal([]byte(`{"severity":"UNKNOWN"}`), &f); err != nil || f.Severity != SevUnknown {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestCategoryIsTheTierLabel(t *testing.T) {
	b, err := json.Marshal(Finding{Key: "k", Tier: TierHotspot})
	if err != nil || !strings.Contains(string(b), `"tier":2,"category":"hotspot"`) {
		t.Fatalf("%s %v", b, err)
	}
}
