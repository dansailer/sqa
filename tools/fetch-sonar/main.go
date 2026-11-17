// Command fetch-sonar downloads the pinned official sonar CLI for a GOOS/GOARCH
// and verifies it against the SHA-256 compiled into sqa
// (internal/sonarcli.PinnedVersion / PinnedChecksum), so the build, CI and the
// release use one source of truth. An existing file with the right digest is
// kept (CI restores it from cache); anything else is replaced.
//
//	go run ./tools/fetch-sonar [-os linux] [-arch amd64] [-o path]
//	go run ./tools/fetch-sonar -print-version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/dansailer/sqa/internal/sonarcli"
)

func defaultOut(goos, arch string) string {
	name := "sonar-" + goos + "-" + arch + sonarcli.ExeSuffix(goos)
	return filepath.Join("internal", "sonarcli", "bundled", name)
}

func main() {
	osName := flag.String("os", "linux", "GOOS of the sonar binary to fetch")
	arch := flag.String("arch", "amd64", "GOARCH of the sonar binary to fetch")
	out := flag.String("o", "", "output path (default internal/sonarcli/bundled/sonar-<os>-<arch>)")
	printVersion := flag.Bool("print-version", false, "print the pinned sonar version and exit")
	flag.Parse()
	if *printVersion {
		fmt.Println(sonarcli.PinnedVersion)
		return
	}
	if *out == "" {
		*out = defaultOut(*osName, *arch)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *osName, *arch, *out); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-sonar:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, goos, arch, out string) error {
	_, platform, err := sonarcli.Platform(goos, arch)
	if err != nil {
		return err
	}
	want := sonarcli.PinnedChecksum(platform)
	if want == "" {
		return fmt.Errorf("no pinned checksum for %s", platform)
	}
	if got, err := sonarcli.FileSHA256(out); err == nil {
		if got == want {
			fmt.Printf("%s: sonar %s already present, sha256 %s verified\n", out, sonarcli.PinnedVersion, got)
			return nil
		}
		fmt.Printf("%s: sha256 %s does not match pinned %s, downloading again\n", out, got, want)
	}
	in := &sonarcli.Installer{BaseURL: sonarcli.DistributionBaseURL, GOOS: goos, GOARCH: arch}
	res, err := in.Install(ctx, sonarcli.PinnedVersion, out)
	if err != nil {
		return err
	}
	fmt.Printf("%s: downloaded %s\n  sha256 %s verified against %s\n", out, res.URL, res.SHA256, res.ChecksumSource)
	return nil
}
