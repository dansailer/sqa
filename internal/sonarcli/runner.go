// Package sonarcli invokes the official SonarSource `sonar` CLI. Every request
// sqa makes to SonarQube goes through this package, so sqa itself never sees,
// stores or prints an authentication token: `sonar` resolves credentials from
// the OS keychain (`sonar auth login`) or from SONARQUBE_CLI_* env vars.
package sonarcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	// KindNotFound means the sonar binary could not be started.
	KindNotFound
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

// Run executes sonar with args and captures stdout and stderr. The child
// inherits sqa's environment so SONARQUBE_CLI_* variables keep working;
// sqa never inspects them.
func (r *Runner) Run(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	if err := checkAllowed(args); err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, r.Bin, args...) //nolint:gosec // bin is the configured sonar CLI
	cmd.Dir = r.Dir
	cmd.Stdin = nil
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	if runErr == nil {
		return out.Bytes(), errb.Bytes(), nil
	}
	e := &Error{Command: commandName(args), Stderr: errb.String(), Err: runErr, ExitCode: -1}
	var exitErr *exec.ExitError
	switch {
	case errors.As(runErr, &exitErr):
		e.ExitCode = exitErr.ExitCode()
		if e.ExitCode == 2 {
			e.Kind = KindUsage
		}
	default:
		e.Kind = KindNotFound
	}
	return out.Bytes(), errb.Bytes(), e
}

// Interactive runs sonar attached to the user's terminal (used for the
// `sqa auth` pass-throughs, where sonar may prompt or open a browser).
func (r *Runner) Interactive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if err := checkAllowed(args); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.Bin, args...) //nolint:gosec // bin is the configured sonar CLI
	cmd.Dir = r.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		e := &Error{Command: commandName(args), Err: err, ExitCode: -1}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			e.ExitCode = exitErr.ExitCode()
			if e.ExitCode == 2 {
				e.Kind = KindUsage
			}
		} else {
			e.Kind = KindNotFound
		}
		return e
	}
	return nil
}

// Resolve picks the sonar binary: an explicit configured path, else the
// managed install from `sqa setup`, else `sonar` on PATH.
func Resolve(configured, managed string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("sonar.path %q: %w", configured, err)
		}
		return configured, nil
	}
	if managed != "" {
		if st, err := os.Stat(managed); err == nil && !st.IsDir() {
			return managed, nil
		}
	}
	p, err := exec.LookPath("sonar")
	if err != nil {
		return "", errors.New("sonar CLI not found: run `sqa setup` (or set sonar.path in ~/.config/sqa/config.yaml)")
	}
	return p, nil
}
