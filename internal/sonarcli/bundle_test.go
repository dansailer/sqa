package sonarcli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeBundle(data string) *Bundle {
	sum := sha256.Sum256([]byte(data))
	return &Bundle{Version: "1.2.3.4", Platform: "linux-x86-64", SHA256: hex.EncodeToString(sum[:]), Data: []byte(data)}
}

func TestBundleExtract(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	b := fakeBundle("#!/bin/sh\necho 1.2.3\n")

	res, err := b.Extract(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Extracted || res.Path != filepath.Join(dir, "sonar-1.2.3.4") || res.SHA256 != b.SHA256 {
		t.Fatalf("%+v", res)
	}
	st, err := os.Stat(res.Path)
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v err %v", st.Mode(), err)
	}
	if got, _ := os.ReadFile(res.Path); string(got) != string(b.Data) {
		t.Fatal("content")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".sonar-extract-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}

	// Verified copy present: no rewrite.
	res, err = b.Extract(dir, false)
	if err != nil || res.Extracted {
		t.Fatalf("second extract %+v %v", res, err)
	}
	// force always rewrites.
	if res, err = b.Extract(dir, true); err != nil || !res.Extracted {
		t.Fatalf("force %+v %v", res, err)
	}
}

func TestBundleExtractReplacesTamperedBinary(t *testing.T) {
	dir := t.TempDir()
	b := fakeBundle("#!/bin/sh\necho 1.2.3\n")
	res, err := b.Extract(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, different content and mtime: the stamp no longer matches, so
	// the file is re-hashed and replaced.
	evil := strings.Replace(string(b.Data), "1.2.3", "6.6.6", 1)
	if err := os.WriteFile(res.Path, []byte(evil), 0o755); err != nil { //nolint:gosec // test
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(res.Path, later, later); err != nil {
		t.Fatal(err)
	}
	res, err = b.Extract(dir, false)
	if err != nil || !res.Extracted {
		t.Fatalf("tampered copy not replaced: %+v %v", res, err)
	}
	if got, _ := os.ReadFile(res.Path); string(got) != string(b.Data) {
		t.Fatal("content not restored")
	}

	// Missing stamp but correct content: re-hash, accept, rewrite stamp.
	if err := os.Remove(stampPath(res.Path)); err != nil {
		t.Fatal(err)
	}
	if res, err = b.Extract(dir, false); err != nil || res.Extracted {
		t.Fatalf("valid copy without stamp re-extracted: %+v %v", res, err)
	}
	if _, err := os.Stat(stampPath(res.Path)); err != nil {
		t.Fatal("stamp not rewritten")
	}
}

func TestBundleExtractRejectsCorruptBundle(t *testing.T) {
	dir := t.TempDir()
	b := fakeBundle("#!/bin/sh\necho 1.2.3\n")
	b.Data = []byte("#!/bin/sh\necho evil\n")
	if _, err := b.Extract(dir, false); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files written for corrupt bundle: %v", entries)
	}
	if _, err := (*Bundle)(nil).Extract(dir, false); err == nil {
		t.Fatal("nil bundle")
	}
	bad := fakeBundle("x")
	bad.Version = "../../evil"
	if _, err := bad.Extract(dir, false); err == nil {
		t.Fatal("path traversal in version accepted")
	}
}

func TestBundleExtractPrunesOldVersions(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"sonar-1.0.0.1", "sonar-1.0.0.1.verified", "sonar"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fakeBundle("new").Extract(dir, false); err != nil {
		t.Fatal(err)
	}
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, " "); got != "sonar sonar-1.2.3.4 sonar-1.2.3.4.verified" {
		t.Fatalf("dir = %s (the setup download `sonar` must be kept)", got)
	}
}

func TestResolverOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	configured := filepath.Join(dir, "configured")
	managed := filepath.Join(dir, "managed")
	for _, p := range []string{configured, managed} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test
			t.Fatal(err)
		}
	}
	b := fakeBundle("#!/bin/sh\necho 1.2.3\n")
	bdir := filepath.Join(dir, "bin")

	r := Resolver{Configured: configured, Bundle: b, BundleDir: bdir, Managed: managed}
	if got, err := r.Resolve(); err != nil || got.Source != SourceConfig || got.Bin != configured {
		t.Fatalf("config: %+v %v", got, err)
	}
	if _, err := os.Stat(bdir); err == nil {
		t.Fatal("bundle extracted although sonar.path wins")
	}

	r.Configured = ""
	loc, err := r.Locate()
	if err != nil || loc.Source != SourceBundled || loc.Bin != filepath.Join(bdir, "sonar-1.2.3.4") {
		t.Fatalf("locate: %+v %v", loc, err)
	}
	if _, err := os.Stat(loc.Bin); err == nil {
		t.Fatal("Locate must not extract")
	}
	got, err := r.Resolve()
	if err != nil || got.Source != SourceBundled || got.Extract == nil || !got.Extract.Extracted {
		t.Fatalf("bundled: %+v %v", got, err)
	}

	r.Bundle = nil
	if got, err := r.Resolve(); err != nil || got.Source != SourceManaged {
		t.Fatalf("managed: %+v %v", got, err)
	}
	r.Managed = filepath.Join(dir, "missing")
	if _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), "sqa setup") {
		t.Fatalf("not found: %v", err)
	}
	r.Configured = filepath.Join(dir, "nope")
	if _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), "sonar.path") {
		t.Fatalf("bad sonar.path: %v", err)
	}
}
