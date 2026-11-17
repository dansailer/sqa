package sonarcli

import (
	"reflect"
	"testing"
)

func TestParseDetected(t *testing.T) {
	d := ParseDetected([]byte("Using auto-detected project key: my-proj\n     Using pull request 42 (auto-detected from branch feat/x)\n"))
	if d.Project != "my-proj" || d.PullRequest != "42" {
		t.Fatalf("%+v", d)
	}
	if d := ParseDetected(nil); d.Project != "" || d.PullRequest != "" {
		t.Fatalf("%+v", d)
	}
}

func TestListIssuesArgs(t *testing.T) {
	got := ListIssuesOptions{Project: "p", PullRequest: "7", File: "a.go", NewCode: true, PageSize: 500, Page: 2}.Args()
	want := []string{"list", "issues", "--format", "json", "--project", "p", "--pull-request", "7", "--file", "a.go", "--new-code", "--page-size", "500", "--page", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestPlatform(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}: "linux-x86-64", {"linux", "arm64"}: "linux-arm64", {"darwin", "arm64"}: "macos-arm64",
	}
	for in, want := range cases {
		_, p, err := Platform(in[0], in[1])
		if err != nil || p != want {
			t.Errorf("%v: %s %v", in, p, err)
		}
		if PinnedChecksum(p) == "" {
			t.Errorf("no pinned checksum for %s", p)
		}
	}
	if _, _, err := Platform("darwin", "amd64"); err == nil {
		t.Error("darwin/amd64 has no official build")
	}
	if _, _, err := Platform("windows", "amd64"); err == nil {
		t.Error("windows unsupported")
	}
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText("<h2>Fix</h2><p>Use <code>a &amp; b</code></p><ul><li>one</li><li>two</li></ul><pre>x := 1</pre>")
	want := "Fix\nUse a & b\n\n- one\n- two\n\n```\nx := 1\n```"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestFilePath(t *testing.T) {
	if FilePath("p:src/a.go", nil) != "src/a.go" {
		t.Fatal("fallback")
	}
	if FilePath("p:x", []Component{{Key: "p:x", Path: "real/x"}}) != "real/x" {
		t.Fatal("components")
	}
}

func TestShortVersion(t *testing.T) {
	if ShortVersion("1.12.0.17622") != "1.12.0" || ShortVersion("1.2") != "1.2" {
		t.Fatal("ShortVersion")
	}
}
