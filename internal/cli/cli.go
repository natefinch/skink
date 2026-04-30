// Package cli wires the skink commands and is the only place that speaks to
// all the core logic packages.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/natefinch/skink/internal/client"
	"github.com/natefinch/skink/internal/paths"
	"github.com/natefinch/skink/internal/skillrepo"
	"github.com/natefinch/skink/internal/syncer"
	"github.com/natefinch/skink/internal/tui"
)

// Prompter is the interactive surface the CLI needs. Tests supply a fake.
type Prompter interface {
	Text(title, prompt, placeholder string) (string, error)
	SingleSelect(title string, items []string) (int, error)
	BrowseSkills(title string, items []tui.BrowseItem) ([]int, error)
	Status(title string, snapshot tui.StatusSnapshot, update func() tui.StatusSnapshot, addRepo tui.StatusAddRepoFunc) (tui.StatusAction, error)
	InteractiveStatus(
		title string,
		snapshot tui.StatusSnapshot,
		update func() tui.StatusSnapshot,
		addRepo tui.StatusAddRepoFunc,
		apply tui.StatusApplyFunc,
	) error
}

type teaPrompter struct{}

func (teaPrompter) Text(title, prompt, placeholder string) (string, error) {
	return tui.RunTextPrompt(title, prompt, placeholder)
}
func (teaPrompter) SingleSelect(title string, items []string) (int, error) {
	return tui.RunSingleSelect(title, items)
}
func (teaPrompter) BrowseSkills(title string, items []tui.BrowseItem) ([]int, error) {
	return tui.RunBrowseSelect(title, items)
}
func (teaPrompter) Status(title string, snapshot tui.StatusSnapshot, update func() tui.StatusSnapshot, addRepo tui.StatusAddRepoFunc) (tui.StatusAction, error) {
	return tui.RunStatus(title, snapshot, update, addRepo)
}
func (teaPrompter) InteractiveStatus(
	title string,
	snapshot tui.StatusSnapshot,
	update func() tui.StatusSnapshot,
	addRepo tui.StatusAddRepoFunc,
	apply tui.StatusApplyFunc,
) error {
	return tui.RunInteractiveStatus(title, snapshot, update, addRepo, apply)
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
		Use:               "skink",
		Short:             "Manage project-declared AI client skills",
		Args:              cobra.NoArgs,
		RunE:              func(cmd *cobra.Command, args []string) error { return a.runStatus(cmd.Context()) },
		SilenceUsage:      true,
		SilenceErrors:     false,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
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
		a.cmdSync(),
	)
	return root
}

func (a *App) projectLibrary(ctx context.Context) (skillrepo.Library, string, error) {
	layout, err := paths.Resolve(a.Env)
	if err != nil {
		return skillrepo.Library{}, "", err
	}
	projectRoot, err := paths.ProjectRoot(a.Env)
	if err != nil {
		return skillrepo.Library{}, "", err
	}
	lib, err := skillrepo.NewLibrary(projectRoot, layout.SkinkHome, a.Git)
	if err != nil {
		if errors.Is(err, skillrepo.ErrConfigNotFound) {
			return lib, projectRoot, fmt.Errorf("no skink config found in %s (expected .skink.yaml, .skink.yml, .skink.json, or .skink.toml)", projectRoot)
		}
		return lib, projectRoot, err
	}
	if err := lib.EnsureCloned(ctx); err != nil {
		return lib, projectRoot, err
	}
	return lib, projectRoot, nil
}

func (a *App) browseRepoWithTarget(ctx context.Context, skinkHome, projectRoot string, cfg skillrepo.Config, rawURL, targetRoot string) error {
	gitURL, repo, err := a.prepareRepo(ctx, skinkHome, rawURL)
	if err != nil {
		return err
	}
	return a.browseSourceRepo(ctx, projectRoot, cfg, gitURL.Original, repo, targetRoot)
}

func (a *App) prepareRepo(ctx context.Context, skinkHome, rawURL string) (skillrepo.GitURL, skillrepo.Repo, error) {
	gitURL, err := skillrepo.ParseGitURL(rawURL)
	if err != nil {
		return skillrepo.GitURL{}, skillrepo.Repo{}, err
	}
	repo := skillrepo.New(filepath.Join(append([]string{skinkHome}, gitURL.CloneDirSegments()...)...), a.Git)
	if !repo.Exists() {
		if err := repo.CloneShallow(ctx, gitURL.CloneURL()); err != nil {
			return skillrepo.GitURL{}, skillrepo.Repo{}, fmt.Errorf("clone: %w", err)
		}
	}
	return gitURL, repo, nil
}

func (a *App) browseSourceRepo(ctx context.Context, projectRoot string, cfg skillrepo.Config, rawURL string, repo skillrepo.Repo, targetRoot string) error {
	selection, err := sourceSkillSelectionFor(cfg, rawURL, repo)
	if err != nil {
		return err
	}
	if len(selection.discovered) == 0 {
		return fmt.Errorf("no SKILL.md files found in %s", repo.Dir)
	}
	idxs, err := a.Prompter.BrowseSkills("Select skills to add:", selection.items)
	if err != nil {
		return err
	}
	return a.applySourceSkillSelection(projectRoot, cfg, rawURL, targetRoot, selection, idxs)
}

type sourceSkillSelection struct {
	discovered []skillrepo.DiscoveredSkill
	knownDirs  []string
	included   map[string]bool
	items      []tui.BrowseItem
}

func sourceSkillSelectionFor(cfg skillrepo.Config, rawURL string, repo skillrepo.Repo) (sourceSkillSelection, error) {
	discovered, err := skillrepo.DiscoverSkills(repo.Dir)
	if err != nil {
		return sourceSkillSelection{}, err
	}
	knownDirs := make([]string, len(discovered))
	for i, s := range discovered {
		knownDirs[i] = s.RelDir
	}
	included := skillrepo.IncludedDirs(cfg, rawURL, knownDirs)
	items := make([]tui.BrowseItem, len(discovered))
	for i, s := range discovered {
		items[i] = tui.BrowseItem{
			Name:        s.Name,
			Path:        s.RelDir,
			Description: s.Description,
			Selected:    included[s.RelDir],
		}
	}
	return sourceSkillSelection{
		discovered: discovered,
		knownDirs:  knownDirs,
		included:   included,
		items:      items,
	}, nil
}

func (a *App) applySourceSkillSelection(
	projectRoot string,
	cfg skillrepo.Config,
	rawURL string,
	targetRoot string,
	selection sourceSkillSelection,
	idxs []int,
) error {
	dirs := make([]string, len(idxs))
	syncItems := make([]syncer.Item, len(idxs))
	for i, idx := range idxs {
		if idx < 0 || idx >= len(selection.discovered) {
			return fmt.Errorf("invalid selection %d", idx)
		}
		s := selection.discovered[idx]
		dirs[i] = s.RelDir
		syncItems[i] = syncer.Item{Name: s.Name, Source: s.Path}
	}
	selectedDirs := map[string]struct{}{}
	for _, dir := range dirs {
		selectedDirs[dir] = struct{}{}
	}
	var removed []string
	for _, s := range selection.discovered {
		if !selection.included[s.RelDir] {
			continue
		}
		if _, ok := selectedDirs[s.RelDir]; ok {
			continue
		}
		removed = append(removed, s.Name)
	}

	cfg = skillrepo.UpdateImportDirs(cfg, rawURL, selection.knownDirs, dirs)
	if err := skillrepo.SaveConfig(projectRoot, cfg); err != nil {
		return err
	}
	if _, err := removeSkillDirs(targetRoot, removed); err != nil {
		return err
	}
	result, err := syncer.Sync(syncItems, targetRoot, false)
	if err != nil {
		return err
	}
	if len(result.Conflicts) > 0 {
		return fmt.Errorf("sync conflicts: %d conflict(s); run skink sync -f to overwrite conflicting skill directories", len(result.Conflicts))
	}
	return nil
}

func (a *App) cmdSync() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Copy configured skills into the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			lib, projectRoot, err := a.projectLibrary(cmd.Context())
			if err != nil {
				return err
			}
			if err := a.pullLibrary(cmd.Context(), lib); err != nil {
				return err
			}
			skills, err := lib.ListAll()
			if err != nil {
				return err
			}
			if len(skills) == 0 {
				return fmt.Errorf("no skills found from skink config in %s", projectRoot)
			}
			targetRoot, err := a.resolveSkillDir(projectRoot, lib.Config)
			if err != nil {
				return err
			}
			result, err := syncer.Sync(syncItemsForSkills(skills), targetRoot, force)
			if err != nil {
				return err
			}
			a.printSyncResult(projectRoot, result)
			if len(result.Conflicts) > 0 {
				return fmt.Errorf("sync conflicts: %d conflict(s); rerun with -f to overwrite conflicting skill directories", len(result.Conflicts))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite existing skill directories that differ from the cache")
	return cmd
}

func syncItemsForSkills(skills []skillrepo.Skill) []syncer.Item {
	items := make([]syncer.Item, len(skills))
	for i, s := range skills {
		items[i] = syncer.Item{Name: s.Name, Source: s.Path}
	}
	return items
}

func (a *App) pullLibrary(ctx context.Context, lib skillrepo.Library) error {
	if len(lib.Sources) == 0 {
		fmt.Fprintln(a.Out, "no skill sources configured")
		return nil
	}
	for _, s := range lib.Sources {
		label := s.URL.DisplayPath()
		if s.Version != "" {
			label += " @ " + s.Version
		}
		fmt.Fprintf(a.Out, "Pulling %s (%s) ...\n", label, s.Repo.Dir)
	}
	return lib.PullAll(ctx)
}

func (a *App) resolveSkillDir(projectRoot string, cfg skillrepo.Config) (string, error) {
	if cfg.SkillDir != "" {
		return filepath.Join(projectRoot, filepath.FromSlash(cfg.SkillDir)), nil
	}

	detected, err := client.Detect(projectRoot)
	if err != nil {
		return "", err
	}
	switch len(detected) {
	case 1:
		return filepath.Join(projectRoot, detected[0].SkillsSubdir), nil
	case 0:
		return a.promptSkillDir(projectRoot, "No client marker found. Pick a skill directory:", client.Registry)
	default:
		return a.promptSkillDir(projectRoot, "Multiple clients detected. Pick a skill directory:", detected)
	}
}

func (a *App) promptSkillDir(projectRoot, title string, clients []client.Client) (string, error) {
	items := make([]string, len(clients))
	for i, c := range clients {
		items[i] = fmt.Sprintf("%s (%s)", c.Name, c.SkillsSubdir)
	}
	idx, err := a.Prompter.SingleSelect(title, items)
	if err != nil {
		return "", err
	}
	if idx < 0 || idx >= len(clients) {
		return "", fmt.Errorf("invalid selection %d", idx)
	}
	return filepath.Join(projectRoot, clients[idx].SkillsSubdir), nil
}

func (a *App) printSyncResult(projectRoot string, result syncer.Result) {
	printPaths := func(title string, paths []string) {
		printPathList(a.Out, projectRoot, title, paths)
	}
	printPaths("added:", result.Added)
	printPaths("already up to date:", result.Existing)
	printPaths("overwritten:", result.Overwritten)
	if len(result.Conflicts) > 0 {
		fmt.Fprintln(a.Out, "conflicts:")
		for _, c := range result.Conflicts {
			fmt.Fprintf(a.Out, "  %s: %s\n", displayPath(projectRoot, c.Path), c.Reason)
		}
	}
}

func removeSkillDirs(targetRoot string, names []string) ([]string, error) {
	var removed []string
	seen := map[string]struct{}{}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		target := filepath.Join(targetRoot, name)
		if _, err := os.Lstat(target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("remove %s: %w", target, err)
		}
		if err := os.RemoveAll(target); err != nil {
			return removed, fmt.Errorf("remove %s: %w", target, err)
		}
		removed = append(removed, target)
	}
	return removed, nil
}

func printPathList(w io.Writer, projectRoot, title string, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", displayPath(projectRoot, p))
	}
}

func displayPath(projectRoot, p string) string {
	rel, err := filepath.Rel(projectRoot, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}
