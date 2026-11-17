// Command fetch-sonar downloads the pinned official sonar CLI for linux/amd64
// and verifies it against the SHA-256 compiled into sqa
// (internal/sonarcli.PinnedVersion / PinnedChecksum), so the build, CI and the
// release use one source of truth. An existing file with the right digest is
// kept (CI restores it from cache); anything else is replaced.
//
//	go run ./tools/fetch-sonar [-o internal/sonarcli/bundled/sonar-linux-amd64]
//	go run ./tools/fetch-sonar -print-version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/dansailer/sqa/internal/sonarcli"
)

const platform = "linux-x86-64"

func main() {
	out := flag.String("o", "internal/sonarcli/bundled/sonar-linux-amd64", "output path")
	printVersion := flag.Bool("print-version", false, "print the pinned sonar version and exit")
	flag.Parse()
	if *printVersion {
		fmt.Println(sonarcli.PinnedVersion)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *out); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-sonar:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out string) error {
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
	in := &sonarcli.Installer{BaseURL: sonarcli.DistributionBaseURL, GOOS: "linux", GOARCH: "amd64"}
	res, err := in.Install(ctx, sonarcli.PinnedVersion, out)
	if err != nil {
		return err
	}
	fmt.Printf("%s: downloaded %s\n  sha256 %s verified against %s\n", out, res.URL, res.SHA256, res.ChecksumSource)
	return nil
}
