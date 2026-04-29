// Package cli wires the skink commands and is the only place that speaks
// both to the tui package and to all the core logic packages.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/natefinch/skink/internal/client"
	"github.com/natefinch/skink/internal/config"
	"github.com/natefinch/skink/internal/installer"
	"github.com/natefinch/skink/internal/paths"
	"github.com/natefinch/skink/internal/skillrepo"
	"github.com/natefinch/skink/internal/tui"
)

// Prompter is the interactive surface the CLI needs. Tests supply a fake.
type Prompter interface {
	Text(title, prompt, placeholder string) (string, error)
	MultiSelect(title string, items []string) ([]int, error)
	SingleSelect(title string, items []string) (int, error)
}

type teaPrompter struct{}

func (teaPrompter) Text(title, prompt, placeholder string) (string, error) {
	return tui.RunTextPrompt(title, prompt, placeholder)
}
func (teaPrompter) MultiSelect(title string, items []string) ([]int, error) {
	return tui.RunMultiSelect(title, items, false)
}
func (teaPrompter) SingleSelect(title string, items []string) (int, error) {
	return tui.RunSingleSelect(title, items)
}

// App holds the injectable dependencies. Zero value is production-ready.
type App struct {
	Env      paths.Env
	Git      skillrepo.GitRunner
	Prompter Prompter
	Out      io.Writer
	Err      io.Writer
}

func (a *App) defaults() {
	if a.Env == nil {
		a.Env = paths.OSEnv{}
	}
	if a.Git == nil {
		a.Git = skillrepo.ExecGit{}
	}
	if a.Prompter == nil {
		a.Prompter = teaPrompter{}
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if a.Err == nil {
		a.Err = os.Stderr
	}
}

// Execute runs the CLI with default dependencies.
func Execute() error {
	app := &App{}
	return app.Root().Execute()
}

// Root returns the root cobra command.
func (a *App) Root() *cobra.Command {
	a.defaults()
	var verbose bool
	root := &cobra.Command{
		Use:           "skink",
		Short:         "Manage AI client skills via symlinks from a personal skills repo",
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				if _, ok := a.Git.(skillrepo.ExecGit); ok {
					a.Git = skillrepo.ExecGit{Verbose: true}
				}
			}
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show git command output")
	root.AddCommand(
		a.cmdInit(),
		a.cmdInstall(),
		a.cmdUninstall(),
		a.cmdList(),
		a.cmdStatus(),
		a.cmdUpdate(),
	)
	return root
}

// ensureConfig loads the config, running the first-run flow if missing.
// It returns the loaded config and the resolved paths.
func (a *App) ensureConfig(ctx context.Context) (config.Config, paths.Layout, error) {
	layout, err := paths.Resolve(a.Env)
	if err != nil {
		return config.Config{}, layout, err
	}
	cfg, err := config.Load(layout.Config)
	if err == nil {
		return cfg, layout, nil
	}
	if !errors.Is(err, config.ErrNotFound) {
		return cfg, layout, err
	}
	// First-run: ask for repo URL and clone.
	fmt.Fprintln(a.Out, "Welcome to skink! Let's get set up.")
	url, err := a.Prompter.Text(
		"First-time setup",
		"Enter the git URL of your skills repo:",
		"git@github.com:me/my-skills.git",
	)
	if err != nil {
		return cfg, layout, err
	}
	cfg = config.Config{SkillsRepo: url, CheckoutDir: layout.Checkout}
	repo := skillrepo.New(cfg.CheckoutDir, a.Git)
	if !repo.Exists() {
		fmt.Fprintf(a.Out, "Cloning %s into %s ...\n", url, cfg.CheckoutDir)
		if err := repo.Clone(ctx, url); err != nil {
			return cfg, layout, fmt.Errorf("clone: %w", err)
		}
	}
	if err := config.Save(layout.Config, cfg); err != nil {
		return cfg, layout, err
	}
	fmt.Fprintf(a.Out, "Saved config to %s\n", layout.Config)
	return cfg, layout, nil
}

func (a *App) cmdInit() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize skink (prompt for skills repo, clone it)",
		RunE: func(cmd *cobra.Command, args []string) error {
			layout, err := paths.Resolve(a.Env)
			if err != nil {
				return err
			}
			if _, err := config.Load(layout.Config); err == nil {
				return fmt.Errorf("skink is already initialized at %s (delete it first to re-init)", layout.Config)
			} else if !errors.Is(err, config.ErrNotFound) {
				return err
			}
			_, _, err = a.ensureConfig(cmd.Context())
			return err
		},
	}
}

// resolveClient returns the client to install into, using auto-detect first
// and prompting only when ambiguous or overridden by --client.
func (a *App) resolveClient(projectRoot, flagName string) (client.Client, error) {
	if flagName != "" {
		c, ok := client.ByName(flagName)
		if !ok {
			return client.Client{}, fmt.Errorf("unknown client %q (known: %s)", flagName, strings.Join(client.Names(), ", "))
		}
		return c, nil
	}
	detected, err := client.Detect(projectRoot)
	if err != nil {
		return client.Client{}, err
	}
	switch len(detected) {
	case 0:
		items := client.Names()
		idx, err := a.Prompter.SingleSelect("No client marker found. Pick one:", items)
		if err != nil {
			return client.Client{}, err
		}
		c, _ := client.ByName(items[idx])
		return c, nil
	case 1:
		return detected[0], nil
	default:
		items := make([]string, len(detected))
		for i, c := range detected {
			items[i] = c.Name
		}
		idx, err := a.Prompter.SingleSelect("Multiple clients detected. Pick one:", items)
		if err != nil {
			return client.Client{}, err
		}
		return detected[idx], nil
	}
}

func (a *App) cmdInstall() *cobra.Command {
	var (
		clientFlag string
		skillFlags []string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install one or more skills into the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, layout, err := a.ensureConfig(cmd.Context())
			if err != nil {
				return err
			}
			projectRoot, err := paths.ProjectRoot(a.Env)
			if err != nil {
				return err
			}
			lib, err := skillrepo.NewLibrary(layout.SkinkHome, a.Git)
			if err != nil {
				return err
			}
			if err := lib.EnsureCloned(cmd.Context()); err != nil {
				return err
			}
			skills, err := lib.ListAll()
			if err != nil {
				return err
			}
			if len(skills) == 0 {
				return fmt.Errorf("no skills found in %s", layout.SkinkHome)
			}

			chosen, err := selectSkills(a.Prompter, skills, skillFlags)
			if err != nil {
				return err
			}
			cli, err := a.resolveClient(projectRoot, clientFlag)
			if err != nil {
				return err
			}
			for _, s := range chosen {
				target := cli.TargetFor(projectRoot, s.InstallSubpath)
				if err := installer.Install(s.Path, target); err != nil {
					return fmt.Errorf("install %s: %w", s.Name, err)
				}
				fmt.Fprintf(a.Out, "installed %s → %s\n", s.Name, target)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&clientFlag, "client", "", "client to install for (skip auto-detect)")
	cmd.Flags().StringSliceVar(&skillFlags, "skill", nil, "skill name(s) to install (repeatable; skips picker)")
	return cmd
}

func selectSkills(p Prompter, all []skillrepo.Skill, byName []string) ([]skillrepo.Skill, error) {
	if len(byName) > 0 {
		index := map[string]skillrepo.Skill{}
		for _, s := range all {
			index[s.Name] = s
		}
		var out []skillrepo.Skill
		for _, n := range byName {
			s, ok := index[n]
			if !ok {
				return nil, fmt.Errorf("unknown skill %q", n)
			}
			out = append(out, s)
		}
		return out, nil
	}
	names := make([]string, len(all))
	for i, s := range all {
		if s.Source != "" {
			names[i] = fmt.Sprintf("%s  (%s)", s.Name, s.Source)
		} else {
			names[i] = s.Name
		}
	}
	idxs, err := p.MultiSelect("Select skills to install:", names)
	if err != nil {
		return nil, err
	}
	out := make([]skillrepo.Skill, len(idxs))
	for i, idx := range idxs {
		out[i] = all[idx]
	}
	return out, nil
}

func (a *App) cmdUninstall() *cobra.Command {
	var clientFlag string
	var skillFlags []string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove previously-installed skill symlinks from the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := a.ensureConfig(cmd.Context())
			if err != nil {
				return err
			}
			projectRoot, err := paths.ProjectRoot(a.Env)
			if err != nil {
				return err
			}
			cli, err := a.resolveClient(projectRoot, clientFlag)
			if err != nil {
				return err
			}
			dir := filepath.Join(projectRoot, cli.SkillsSubdir)
			installed, err := installer.ListInstalled(dir)
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				fmt.Fprintf(a.Out, "no installed skills in %s\n", dir)
				return nil
			}

			var chosen []installer.Status
			if len(skillFlags) > 0 {
				m := map[string]installer.Status{}
				for _, s := range installed {
					m[s.Name] = s
					m[s.RelPath] = s
				}
				for _, n := range skillFlags {
					s, ok := m[n]
					if !ok {
						return fmt.Errorf("skill %q is not installed in %s", n, dir)
					}
					chosen = append(chosen, s)
				}
			} else {
				names := make([]string, len(installed))
				for i, s := range installed {
					names[i] = s.RelPath
				}
				idxs, err := a.Prompter.MultiSelect("Select skills to uninstall:", names)
				if err != nil {
					return err
				}
				for _, idx := range idxs {
					chosen = append(chosen, installed[idx])
				}
			}
			for _, s := range chosen {
				if err := installer.Remove(s.Target); err != nil {
					return fmt.Errorf("uninstall %s: %w", s.RelPath, err)
				}
				fmt.Fprintf(a.Out, "removed %s\n", s.RelPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&clientFlag, "client", "", "client to uninstall from (skip auto-detect)")
	cmd.Flags().StringSliceVar(&skillFlags, "skill", nil, "skill name(s) to uninstall (repeatable; skips picker)")
	return cmd
}

func (a *App) cmdList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available skills and whether each is installed in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, layout, err := a.ensureConfig(cmd.Context())
			if err != nil {
				return err
			}
			lib, err := skillrepo.NewLibrary(layout.SkinkHome, a.Git)
			if err != nil {
				return err
			}
			if err := lib.EnsureCloned(cmd.Context()); err != nil {
				return err
			}
			skills, err := lib.ListAll()
			if err != nil {
				return err
			}
			projectRoot, err := paths.ProjectRoot(a.Env)
			if err != nil {
				return err
			}

			installed := map[string][]string{} // skill name -> client names
			for _, c := range client.Registry {
				st, err := installer.ListInstalled(filepath.Join(projectRoot, c.SkillsSubdir))
				if err != nil {
					return err
				}
				for _, s := range st {
					installed[s.Name] = append(installed[s.Name], c.Name)
				}
			}
			for _, s := range skills {
				marker := "  "
				var parts []string
				if s.Source != "" {
					parts = append(parts, "from "+s.Source)
				}
				if clients, ok := installed[s.Name]; ok {
					marker = "✓ "
					sort.Strings(clients)
					parts = append(parts, "installed: "+strings.Join(clients, ", "))
				}
				tail := ""
				if len(parts) > 0 {
					tail = "  (" + strings.Join(parts, "; ") + ")"
				}
				fmt.Fprintf(a.Out, "%s%s%s\n", marker, s.Name, tail)
			}
			return nil
		},
	}
}

func (a *App) cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show skills currently installed in the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := paths.ProjectRoot(a.Env)
			if err != nil {
				return err
			}
			any := false
			for _, c := range client.Registry {
				dir := filepath.Join(projectRoot, c.SkillsSubdir)
				st, err := installer.ListInstalled(dir)
				if err != nil {
					return err
				}
				if len(st) == 0 {
					continue
				}
				any = true
				fmt.Fprintf(a.Out, "[%s] %s\n", c.Name, dir)
				for _, s := range st {
					state := "ok"
					if s.IsDangling {
						state = "DANGLING"
					}
					fmt.Fprintf(a.Out, "  %s -> %s  [%s]\n", s.RelPath, s.LinkDest, state)
				}
			}
			if !any {
				fmt.Fprintln(a.Out, "no skills installed in this project")
			}
			return nil
		},
	}
}

func (a *App) cmdUpdate() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update the skills repo checkout and any imports (git pull --ff-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, layout, err := a.ensureConfig(cmd.Context())
			if err != nil {
				return err
			}
			lib, err := skillrepo.NewLibrary(layout.SkinkHome, a.Git)
			if err != nil {
				return err
			}
			// Pick up any imports the user has added since the last update,
			// and any new imports introduced by pulling the primary repo.
			if err := lib.EnsureCloned(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "Pulling %s ...\n", lib.Primary.Dir)
			for _, s := range lib.Sources {
				fmt.Fprintf(a.Out, "Pulling %s (%s) ...\n", s.URL.DisplayPath(), s.Repo.Dir)
			}
			if err := lib.PullAll(cmd.Context()); err != nil {
				return err
			}
			// After pulling primary, its skink config may now declare new
			// imports — clone those too.
			lib, err = skillrepo.NewLibrary(layout.SkinkHome, a.Git)
			if err != nil {
				return err
			}
			return lib.EnsureCloned(cmd.Context())
		},
	}
}
