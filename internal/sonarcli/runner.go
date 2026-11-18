// Package sonarcli invokes the official SonarSource `sonar` CLI. Every request
// sqa makes to SonarQube goes through this package, so sqa itself never sees,
// reads, stores or prints an authentication token: `sonar` resolves
// credentials from SONARQUBE_CLI_* env vars or from its token store (by
// default a 0600 file that sqa points it at, see Environment).
package sonarcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ErrorKind classifies a failed sonar invocation so commands can map it to
// sqa's exit codes.
type ErrorKind int

const (
	// KindServer covers auth, network and HTTP errors reported by sonar.
	KindServer ErrorKind = iota
	// KindUsage means sonar rejected the arguments (sonar exit code 2).
	KindUsage
	// KindNotStarted means the sonar binary could not be started.
	KindNotStarted
)

// Error is returned when sonar exits non-zero or cannot be started.
type Error struct {
	Kind     ErrorKind
	Command  string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("sonar %s failed (exit %d): %s", e.Command, e.ExitCode, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// reHTTPStatus matches how `sonar api` reports an HTTP error on stderr.
var reHTTPStatus = regexp.MustCompile(`API error: (\d{3})\b`)

// HTTPStatus is the HTTP status of a failed `sonar api` call, or 0.
func (e *Error) HTTPStatus() int {
	m := reHTTPStatus.FindStringSubmatch(e.Stderr)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ErrForbidden is returned when sqa is asked to run a sonar subcommand that it
// must never run (for example `sonar integrate`, which installs MCP servers).
var ErrForbidden = errors.New("forbidden sonar subcommand")

// Runner runs a sonar binary.
type Runner struct {
	// Bin is the path to the sonar executable.
	Bin string
	// Dir is the working directory (empty = current directory). sonar uses it
	// for project auto-detection.
	Dir string
	// Env, when set, is applied to sonar's environment: token store, sonar
	// home, and the permission checks around each run.
	Env *Environment
}

// forbidden lists sonar subcommands sqa refuses to run under any
// circumstances. `integrate` installs MCP servers and agent hooks; `remediate`
// hands issues to a remote AI agent without user review.
var forbidden = map[string]bool{"integrate": true, "remediate": true}

func checkAllowed(args []string) error {
	if len(args) > 0 && forbidden[args[0]] {
		return fmt.Errorf("%w: sonar %s", ErrForbidden, args[0])
	}
	return nil
}

func commandName(args []string) string {
	n := 0
	for i, a := range args {
		if strings.HasPrefix(a, "-") || i >= 3 {
			break
		}
		n = i + 1
	}
	return strings.Join(args[:n], " ")
}

// command builds the exec.Cmd and prepares the token store. The child
// inherits sqa's environment so SONARQUBE_CLI_* variables keep working; sqa
// never inspects their values.
func (r *Runner) command(ctx context.Context, args []string) (*exec.Cmd, error) {
	if err := checkAllowed(args); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.Bin, args...) //nolint:gosec // bin is the configured sonar CLI
	cmd.Dir = r.Dir
	if r.Env != nil {
		if err := r.Env.Prepare(); err != nil {
			return nil, err
		}
		cmd.Env = r.Env.Env(os.Environ())
	}
	return cmd, nil
}

// secure re-applies 0600 to the credentials file after sonar ran. Its error
// is only reported when sonar itself succeeded.
func (r *Runner) secure() error {
	if r.Env == nil {
		return nil
	}
	_, err := r.Env.Secure()
	return err
}

// Run executes sonar with args and captures stdout and stderr.
func (r *Runner) Run(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	cmd, err := r.command(ctx, args)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdin = nil
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	secErr := r.secure()
	if runErr == nil {
		return out.Bytes(), errb.Bytes(), secErr
	}
	return out.Bytes(), errb.Bytes(), runError(args, errb.String(), runErr)
}

// runError classifies a failed sonar run: exit 2 is a usage error, a
// process that never started is KindNotStarted, anything else KindServer.
func runError(args []string, stderr string, err error) *Error {
	e := &Error{Command: commandName(args), Stderr: stderr, Err: err, ExitCode: -1, Kind: KindNotStarted}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		e.ExitCode, e.Kind = exitErr.ExitCode(), KindServer
		if e.ExitCode == 2 {
			e.Kind = KindUsage
		}
	}
	return e
}

// Interactive runs sonar attached to the user's terminal (used for the
// `sqa auth` pass-throughs, where sonar may prompt or open a browser).
func (r *Runner) Interactive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd, err := r.command(ctx, args)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	runErr := cmd.Run()
	secErr := r.secure()
	if runErr != nil {
		return runError(args, "", runErr)
	}
	return secErr
}

// Source names where the sonar binary came from.
type Source string

// Binary sources, in resolution order.
const (
	SourceConfig  Source = "sonar.path"
	SourceBundled Source = "bundled"
	SourceManaged Source = "sqa setup"
	SourcePATH    Source = "PATH"
)

// Resolver picks the sonar binary. Order: an explicit configured path, else
// the binary bundled into sqa (extracted on first use), else the managed
// install from `sqa setup`, else `sonar` on PATH.
type Resolver struct {
	Configured string  // sonar.path from config
	Bundle     *Bundle // nil when not bundled (or overridden by sonar.version)
	BundleDir  string  // where the bundle is extracted
	Managed    string  // `sqa setup` download location
}

// Resolved is the chosen sonar binary.
type Resolved struct {
	Bin    string
	Source Source
	// Extract is set when Source is SourceBundled and Resolve extracted.
	Extract *ExtractResult
}

// Resolve returns the binary to run, extracting the bundle when needed.
func (r Resolver) Resolve() (*Resolved, error) { return r.resolve(true) }

// Locate reports which binary Resolve would use without writing anything.
// For a bundle, Bin is the extraction target, which may not exist yet.
func (r Resolver) Locate() (*Resolved, error) { return r.resolve(false) }

func (r Resolver) resolve(extract bool) (*Resolved, error) {
	if r.Configured != "" {
		if _, err := os.Stat(r.Configured); err != nil {
			return nil, fmt.Errorf("sonar.path %q: %w", r.Configured, err)
		}
		return &Resolved{Bin: r.Configured, Source: SourceConfig}, nil
	}
	if r.Bundle != nil {
		if !extract {
			return &Resolved{Bin: filepath.Join(r.BundleDir, r.Bundle.FileName()), Source: SourceBundled}, nil
		}
		res, err := r.Bundle.Extract(r.BundleDir, false)
		if err != nil {
			return nil, fmt.Errorf("bundled sonar: %w", err)
		}
		return &Resolved{Bin: res.Path, Source: SourceBundled, Extract: res}, nil
	}
	if r.Managed != "" {
		if st, err := os.Stat(r.Managed); err == nil && !st.IsDir() {
			return &Resolved{Bin: r.Managed, Source: SourceManaged}, nil
		}
	}
	p, err := exec.LookPath("sonar")
	if err != nil {
		return nil, errors.New("sonar CLI not found: run `sqa setup` (or set sonar.path in ~/.config/sqa/config.yaml)")
	}
	return &Resolved{Bin: p, Source: SourcePATH}, nil
}
