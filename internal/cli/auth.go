package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/sonarcli"
)

type authStatus struct{ sonarcli.AuthStatus }

func (s authStatus) Markdown() string { return s.Table() }
func (s authStatus) Table() string {
	out := "status: " + s.Status
	if s.Server != "" {
		out += "\nserver: " + s.Server
	}
	if s.Source != "" {
		out += "\nsource: " + s.Source
	}
	if s.Message != "" {
		out += "\nmessage: " + s.Message
	}
	return out
}

func (a *app) authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication, delegated entirely to `sonar auth` (sqa never sees tokens)",
	}
	pass := func(use, short string) *cobra.Command {
		return &cobra.Command{
			Use:                use,
			Short:              short,
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				for _, x := range args {
					if x == "-h" || x == "--help" {
						return cmd.Help()
					}
				}
				r, err := a.runner()
				if err != nil {
					return err
				}
				return r.Interactive(cmd.Context(), a.stdin, a.stdout, a.stderr, append([]string{"auth", cmd.Name()}, args...)...)
			},
		}
	}
	login := pass("login [sonar auth login flags]", "Run `sonar auth login` (browser flow; the user must run this in a terminal)")
	login.Long = `Runs the official 'sonar auth login' with the given flags, e.g.

  sqa auth login --server https://sonarqube.example.com

The token is stored by sonar in the OS keychain. For CI, set SONARQUBE_CLI_SERVER
and SONARQUBE_CLI_TOKEN instead. Coding agents must not run this command.`
	logout := pass("logout [sonar auth logout flags]", "Run `sonar auth logout`")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the active sonar connection (`sonar auth status --format json`)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.runner()
			if err != nil {
				return err
			}
			st, err := r.AuthStatus(cmd.Context())
			if err != nil {
				return err
			}
			if err := a.out(authStatus{st}); err != nil {
				return err
			}
			if !st.Connected() {
				return &ExitError{Code: ExitServer, Err: fmt.Errorf("not connected: %s", st.Status)}
			}
			return nil
		},
	}
	cmd.AddCommand(login, status, logout)
	return cmd
}
