// Package cli implements the sqa command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/approval"
	"github.com/dansailer/sqa/internal/config"
	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// Set at build time via -ldflags "-X github.com/dansailer/sqa/internal/cli.version=...".
var (
	version = "dev"
	commit  = ""
)

// Exit codes (DESIGN §6).
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitUsage    = 2
	ExitServer   = 3
	ExitApproval = 4
)

// ExitError carries an exit code through cobra.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

func usageErr(format string, a ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, a...)}
}

// app holds per-invocation state.
type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	format         string
	cfg            *config.Config
}

// Version returns the build version.
func Version() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// Run executes sqa with args and returns the process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	code := exitCode(err)
	if code != ExitFindings && !errors.Is(err, errSilent) {
		_, _ = fmt.Fprintf(stderr, "sqa: %v\n", err)
	}
	return code
}

var errSilent = errors.New("silent")

func exitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if errors.Is(err, approval.ErrMissing) {
		return ExitApproval
	}
	if errors.Is(err, sonarcli.ErrForbidden) {
		return ExitUsage
	}
	var se *sonarcli.Error
	if errors.As(err, &se) {
		switch se.Kind {
		case sonarcli.KindUsage, sonarcli.KindNotFound:
			return ExitUsage
		default:
			return ExitServer
		}
	}
	// cobra flag/arg errors
	return ExitUsage
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "sqa",
		Short: "SonarQube findings for coding agents, security first, user-approved triage",
		Long: `sqa wraps the official SonarSource sonar CLI to list SonarQube Server findings
ranked security first (vulnerabilities, hotspots, reliability, maintainability),
explain them, and record user-approved triage decisions.

All server traffic goes through 'sonar'; sqa never handles tokens and never uses MCP.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			a.cfg = cfg
			return nil
		},
	}
	root.PersistentFlags().StringVar(&a.format, "format", "", "output format: toon|json|md|table (default: table on a TTY, toon otherwise)")
	root.AddCommand(
		a.setupCmd(), a.doctorCmd(), a.authCmd(), a.findingsCmd(), a.nextCmd(), a.showCmd(),
		a.markCmd(), a.hotspotCmd(), a.skillCmd(), a.versionCmd(),
	)
	return root
}

// out renders v in the selected format.
func (a *app) out(v any) error {
	f, err := output.Resolve(a.format, a.stdout)
	if err != nil {
		return &ExitError{Code: ExitUsage, Err: err}
	}
	return output.Write(a.stdout, f, v)
}

// runner resolves the sonar binary.
func (a *app) runner() (*sonarcli.Runner, error) {
	managed, err := config.ManagedSonarPath()
	if err != nil {
		return nil, &ExitError{Code: ExitUsage, Err: err}
	}
	bin, err := sonarcli.Resolve(a.cfg.Sonar.Path, managed)
	if err != nil {
		return nil, &ExitError{Code: ExitUsage, Err: err}
	}
	return &sonarcli.Runner{Bin: bin}, nil
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the sqa version and the pinned sonar CLI version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.out(versionInfo{Version: Version(), Commit: commit, PinnedSonar: a.sonarVersion()})
		},
	}
}

func (a *app) sonarVersion() string {
	if a.cfg != nil && a.cfg.Sonar.Version != "" {
		return a.cfg.Sonar.Version
	}
	return sonarcli.PinnedVersion
}

type versionInfo struct {
	Version     string `json:"version"`
	Commit      string `json:"commit,omitempty"`
	PinnedSonar string `json:"sonar_cli"`
}

func (v versionInfo) Markdown() string { return v.Table() }
func (v versionInfo) Table() string {
	s := "sqa " + v.Version
	if v.Commit != "" {
		s += " (" + v.Commit + ")"
	}
	return s + "\nsonar CLI (pinned): " + v.PinnedSonar
}

// Main is the entry point used by cmd/sqa.
func Main() {
	os.Exit(Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
