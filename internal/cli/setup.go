package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/config"
	"github.com/dansailer/sqa/internal/output"
	"github.com/dansailer/sqa/internal/sonarcli"
)

// distributionBaseURL is a var so tests can point setup at a local server.
var distributionBaseURL = sonarcli.DistributionBaseURL

type setupResult struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Path    string `json:"path"`
	*sonarcli.InstallResult
	Note string `json:"note,omitempty"`
}

func (s setupResult) Markdown() string { return s.Table() }
func (s setupResult) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "sonar CLI %s %s at %s\n", s.Version, s.Status, s.Path)
	if s.InstallResult != nil {
		fmt.Fprintf(&b, "source:   %s\nsha256:   %s (verified against %s)\n", s.URL, s.SHA256, s.ChecksumSource)
	}
	if s.Note != "" {
		b.WriteString(s.Note + "\n")
	}
	return b.String()
}

func (a *app) setupCmd() *cobra.Command {
	var force bool
	var ver string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install the pinned official sonar CLI (checksum-verified) into ~/.local/share/sqa/bin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ver == "" {
				ver = a.sonarVersion()
			}
			dest, err := config.ManagedSonarPath()
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			res := setupResult{Version: ver, Path: dest}
			if a.cfg.Sonar.Path != "" {
				res.Note = "note: sonar.path in config.yaml overrides the managed install (" + a.cfg.Sonar.Path + ")"
			}
			if !force && ver == sonarcli.PinnedVersion {
				if ok := alreadyInstalled(dest); ok {
					res.Status = "already installed"
					return a.out(res)
				}
			}
			in := &sonarcli.Installer{BaseURL: distributionBaseURL, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
			ir, err := in.Install(cmd.Context(), ver, dest)
			if err != nil {
				return &ExitError{Code: ExitServer, Err: err}
			}
			res.InstallResult = ir
			res.Status = "installed"
			r := &sonarcli.Runner{Bin: dest}
			got, err := r.Version(cmd.Context())
			if err != nil {
				return &ExitError{Code: ExitServer, Err: fmt.Errorf("installed sonar does not run: %w", err)}
			}
			if got != sonarcli.ShortVersion(ver) {
				res.Note = strings.TrimSpace(res.Note + "\nwarning: sonar --version reports " + got)
			}
			return a.out(res)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if the pinned version is present")
	cmd.Flags().StringVar(&ver, "sonar-version", "", "sonar CLI version to install (default: pinned "+sonarcli.PinnedVersion+" or sonar.version from config)")
	return cmd
}

func alreadyInstalled(dest string) bool {
	if _, err := os.Stat(dest); err != nil {
		return false
	}
	_, platform, err := sonarcli.Platform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false
	}
	sum, err := sonarcli.FileSHA256(dest)
	return err == nil && sum == sonarcli.PinnedChecksum(platform)
}

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorResult struct {
	OK     bool    `json:"ok"`
	Checks []check `json:"checks"`
}

func (d doctorResult) Markdown() string {
	var b strings.Builder
	b.WriteString("| check | status | detail |\n|---|---|---|\n")
	for _, c := range d.Checks {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", c.Name, c.Status, output.MDEscape(c.Detail))
	}
	return b.String()
}

func (d doctorResult) Table() string {
	var b strings.Builder
	for _, c := range d.Checks {
		fmt.Fprintf(&b, "%-5s %-15s %s\n", strings.ToUpper(c.Status), c.Name, c.Detail)
	}
	return b.String()
}

func (a *app) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check sonar install, authentication and server version (>= 2025.1)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			res := doctorResult{OK: true}
			code := ExitOK
			add := func(name, status, detail string, failCode int) {
				res.Checks = append(res.Checks, check{name, status, detail})
				if status == "fail" {
					res.OK = false
					if code == ExitOK {
						code = failCode
					}
				}
			}
			r, err := a.runner()
			if err != nil {
				add("sonar", "fail", err.Error(), ExitUsage)
				return a.finishDoctor(res, code)
			}
			v, err := r.Version(ctx)
			switch {
			case err != nil:
				add("sonar", "fail", r.Bin+": "+err.Error(), ExitUsage)
				return a.finishDoctor(res, code)
			case v != sonarcli.ShortVersion(a.sonarVersion()):
				add("sonar", "warn", fmt.Sprintf("%s is version %s, sqa is tested with %s (run `sqa setup`)", r.Bin, v, a.sonarVersion()), 0)
			default:
				add("sonar", "ok", fmt.Sprintf("%s (version %s)", r.Bin, v), 0)
			}
			st, err := r.AuthStatus(ctx)
			switch {
			case err != nil:
				add("auth", "fail", err.Error(), ExitServer)
				return a.finishDoctor(res, code)
			case !st.Connected():
				msg := "sonar auth status: " + st.Status
				if st.Message != "" {
					msg += " (" + st.Message + ")"
				}
				add("auth", "fail", msg+"; the user must run `sqa auth login --server <url>` in a terminal (or set SONARQUBE_CLI_SERVER and SONARQUBE_CLI_TOKEN)", ExitServer)
				return a.finishDoctor(res, code)
			default:
				add("auth", "ok", fmt.Sprintf("connected to %s via %s", st.Server, st.Source), 0)
			}
			sv, err := r.ServerVersion(ctx)
			switch {
			case err != nil:
				add("server", "fail", err.Error(), ExitServer)
			case !ServerVersionSupported(sv):
				add("server", "fail", "SonarQube "+sv+" is older than 2025.1", ExitServer)
			default:
				add("server", "ok", "SonarQube "+sv, 0)
			}
			return a.finishDoctor(res, code)
		},
	}
}

func (a *app) finishDoctor(res doctorResult, code int) error {
	if err := a.out(res); err != nil {
		return err
	}
	if code != ExitOK {
		return &ExitError{Code: code, Err: errSilent}
	}
	return nil
}

// ServerVersionSupported reports whether a SonarQube version string is
// SonarQube Server 2025.1+ (or Community Build 25.1+).
func ServerVersionSupported(v string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(v, "%d.%d", &major, &minor); err != nil {
		return false
	}
	switch {
	case major >= 2025:
		return true
	case major >= 25 && major < 100:
		return true
	}
	return false
}
