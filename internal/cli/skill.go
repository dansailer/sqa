package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dansailer/sqa/internal/config"
	"github.com/dansailer/sqa/skill"
)

// SkillTargets maps --target names to skills directories relative to the home
// directory (user scope) and to the project root (--project).
var SkillTargets = map[string]struct{ User, Project string }{
	"agents":   {User: ".agents/skills", Project: ".agents/skills"},
	"claude":   {User: ".claude/skills", Project: ".claude/skills"},
	"opencode": {User: ".config/opencode/skills", Project: ".opencode/skills"},
	"cursor":   {User: ".cursor/skills", Project: ".cursor/skills"},
}

// SkillPath resolves where `sqa skill install` writes SKILL.md.
func SkillPath(target string, project bool, cfg *config.Config, home, cwd string) (string, error) {
	if target == "" {
		target = "agents"
	}
	t, known := SkillTargets[target]
	var dir string
	switch {
	case known && project:
		dir = filepath.Join(cwd, t.Project)
	case known && target == "agents" && cfg != nil && cfg.Skills.DefaultDir != "":
		dir = cfg.Skills.DefaultDir
	case known:
		dir = filepath.Join(home, t.User)
	case project:
		return "", fmt.Errorf("--project only works with a named target (agents, claude, opencode, cursor), not a path")
	default:
		p, err := config.ExpandHome(target)
		if err != nil {
			return "", err
		}
		dir, err = filepath.Abs(p)
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "sqa", "SKILL.md"), nil
}

type skillInstallResult struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	Bytes  int    `json:"bytes"`
}

func (s skillInstallResult) Table() string {
	return fmt.Sprintf("installed sqa skill (%s) to %s", s.Target, s.Path)
}

func (a *app) skillCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "skill", Short: "Print or install the embedded SKILL.md for coding agents"}
	cmd.AddCommand(&cobra.Command{
		Use:   "print",
		Short: "Print the embedded SKILL.md",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := a.stdout.Write(skill.Content)
			return err
		},
	})
	var target string
	var project bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Write sqa/SKILL.md into a harness skills directory",
		Long: `Writes the embedded SKILL.md (it never touches MCP configuration).

Targets (user scope / with --project):
  agents    ~/.agents/skills/sqa      ./.agents/skills/sqa   (default; Codex, Cursor, OpenCode, Pi, ...)
  claude    ~/.claude/skills/sqa      ./.claude/skills/sqa
  opencode  ~/.config/opencode/skills/sqa  ./.opencode/skills/sqa
  cursor    ~/.cursor/skills/sqa      ./.cursor/skills/sqa
  <path>    <path>/sqa/SKILL.md`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := config.Home()
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			cwd, err := os.Getwd()
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			p, err := SkillPath(target, project, a.cfg, home, cwd)
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			if err := os.WriteFile(p, skill.Content, 0o644); err != nil { //nolint:gosec // skill file is meant to be readable
				return &ExitError{Code: ExitUsage, Err: err}
			}
			t := target
			if t == "" {
				t = "agents"
			}
			return a.out(skillInstallResult{Path: p, Target: t, Bytes: len(skill.Content)})
		},
	}
	install.Flags().StringVar(&target, "target", "agents", "agents|claude|opencode|cursor|<path>")
	install.Flags().BoolVar(&project, "project", false, "install into the current project instead of the home directory")
	cmd.AddCommand(install)
	return cmd
}
