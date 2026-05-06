package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/natefinch/skink/internal/paths"
	"github.com/natefinch/skink/internal/skillrepo"
	"github.com/natefinch/skink/internal/syncer"
	"github.com/natefinch/skink/internal/tui"
)

type statusScopePage struct {
	root       string // project root (for project) or skink home (for global)
	targetRoot string
	config     skillrepo.Config
	repos      map[string]statusRepoAction
	addedRepos map[string]sourceSkillSelection
	skills     map[string]statusSkillAction
}

type statusPage struct {
	snapshot  tui.StatusSnapshot
	skinkHome string
	scopes    map[tui.Scope]*statusScopePage
}

type statusRepoAction struct {
	source           skillrepo.Source
	selection        sourceSkillSelection
	latestSemverTag  string
	semverComparable bool
}

type statusSkillAction struct {
	repoID    string
	name      string
	sourceDir string
	sourceURL string
	source    string
}

func (p statusPage) scopePage(scope tui.Scope) *statusScopePage {
	return p.scopes[scope]
}

func (a *App) runStatus(ctx context.Context) error {
	if a.Global {
		layout, err := paths.Resolve(a.Env)
		if err != nil {
			return err
		}
		_, found, err := skillrepo.ReadGlobalImports(layout.SkinkHome)
		if err != nil {
			return err
		}
		if !found {
			if err := a.bootstrapGlobalConfig(ctx, layout); err != nil {
				return err
			}
		}
	}
	page, err := a.buildStatusPage(ctx, "")
	if err != nil {
		return err
	}
	updateCtx, cancelUpdates := context.WithCancel(ctx)
	defer cancelUpdates()
	return a.Prompter.InteractiveStatus(
		"Synced Skills",
		page.snapshot,
		statusUpdate(updateCtx, page),
		a.statusAddRepo(ctx, &page),
		a.statusApply(ctx, &page),
	)
}

func (a *App) bootstrapGlobalConfig(ctx context.Context, layout paths.Layout) error {
	configPath := filepath.Join(layout.SkinkHome, ".skink.toml")
	cfg := skillrepo.Config{SkillDir: paths.DefaultGlobalSkillDir}
	if err := skillrepo.SaveConfigAt(configPath, cfg); err != nil {
		return fmt.Errorf("create global config: %w", err)
	}
	fmt.Fprintf(a.Out, "Created global config at %s\n", configPath)
	fmt.Fprintf(a.Out, "Skills will be synced to %s\n", layout.GlobalSkillDir(""))
	return nil
}

func (a *App) statusApply(ctx context.Context, page *statusPage) tui.StatusApplyFunc {
	return func(action tui.StatusAction) (tui.StatusSnapshot, error) {
		if action.Kind == "" || action.Kind == tui.StatusActionQuit {
			return page.snapshot, nil
		}
		message, err := a.handleStatusAction(ctx, *page, action)
		if err != nil {
			return tui.StatusSnapshot{}, err
		}
		next, err := a.buildStatusPage(ctx, message)
		if err != nil {
			return tui.StatusSnapshot{}, err
		}
		*page = next
		return page.snapshot, nil
	}
}

func (a *App) buildStatusPage(ctx context.Context, message string) (statusPage, error) {
	layout, err := paths.Resolve(a.Env)
	if err != nil {
		return statusPage{}, err
	}

	page := statusPage{
		skinkHome: layout.SkinkHome,
		scopes:    map[tui.Scope]*statusScopePage{},
	}
	page.snapshot.Message = message

	// Load global config if available.
	if !a.Global {
		// When not in --global mode, still try to load global for combined TUI.
		globalCfg, globalFound, err := skillrepo.ReadGlobalImports(layout.SkinkHome)
		if err != nil {
			return statusPage{}, err
		}
		if globalFound {
			sp, sec, err := a.buildScopePage(ctx, layout, tui.ScopeGlobal, layout.SkinkHome, globalCfg)
			if err != nil {
				return statusPage{}, err
			}
			page.scopes[tui.ScopeGlobal] = sp
			page.snapshot.Sections = append(page.snapshot.Sections, sec)
		}
	} else {
		// --global mode: only global.
		globalCfg, globalFound, err := skillrepo.ReadGlobalImports(layout.SkinkHome)
		if err != nil {
			return statusPage{}, err
		}
		if !globalFound {
			// Bootstrap will be handled by the caller; return empty.
			sp := &statusScopePage{
				root:       layout.SkinkHome,
				targetRoot: layout.GlobalSkillDir(""),
				config:     skillrepo.Config{},
				repos:      map[string]statusRepoAction{},
				addedRepos: map[string]sourceSkillSelection{},
				skills:     map[string]statusSkillAction{},
			}
			page.scopes[tui.ScopeGlobal] = sp
			page.snapshot.Sections = append(page.snapshot.Sections, tui.StatusSection{
				Scope: tui.ScopeGlobal,
				Title: "Global Skills",
			})
			return page, nil
		}
		sp, sec, err := a.buildScopePage(ctx, layout, tui.ScopeGlobal, layout.SkinkHome, globalCfg)
		if err != nil {
			return statusPage{}, err
		}
		page.scopes[tui.ScopeGlobal] = sp
		page.snapshot.Sections = append(page.snapshot.Sections, sec)
		return page, nil
	}

	// Load project config.
	projectRoot, err := paths.ProjectRoot(a.Env)
	if err != nil {
		// If we have global skills, show them alone.
		if _, hasGlobal := page.scopes[tui.ScopeGlobal]; hasGlobal {
			return page, nil
		}
		return statusPage{}, err
	}
	projectCfg, projectErr := skillrepo.ReadImports(projectRoot)
	if projectErr != nil {
		if errors.Is(projectErr, skillrepo.ErrConfigNotFound) {
			// No project config — show global-only if available.
			if _, hasGlobal := page.scopes[tui.ScopeGlobal]; hasGlobal {
				return page, nil
			}
			return statusPage{}, fmt.Errorf("no skink config found in %s (expected .skink.yaml, .skink.yml, .skink.json, or .skink.toml)", projectRoot)
		}
		return statusPage{}, projectErr
	}

	sp, sec, err := a.buildScopePage(ctx, layout, tui.ScopeProject, projectRoot, projectCfg)
	if err != nil {
		return statusPage{}, err
	}
	page.scopes[tui.ScopeProject] = sp
	page.snapshot.Sections = append(page.snapshot.Sections, sec)

	// Check for duplicate skill names across scopes.
	addDuplicateWarnings(&page)

	return page, nil
}

// addDuplicateWarnings checks for skills with the same name across global and
// project scopes and appends a warning to the snapshot message.
func addDuplicateWarnings(page *statusPage) {
	if len(page.snapshot.Sections) < 2 {
		return
	}
	names := map[string]tui.Scope{}
	var dupes []string
	for _, sec := range page.snapshot.Sections {
		for _, repo := range sec.Repos {
			for _, skill := range repo.Skills {
				if prev, ok := names[skill.Name]; ok && prev != sec.Scope {
					dupes = append(dupes, skill.Name)
				} else {
					names[skill.Name] = sec.Scope
				}
			}
		}
	}
	if len(dupes) == 0 {
		return
	}
	msg := fmt.Sprintf("⚠️  Duplicate skill(s) in both global and project: %s", strings.Join(dupes, ", "))
	if page.snapshot.Message != "" {
		page.snapshot.Message += "\n"
	}
	page.snapshot.Message += msg
}

func (a *App) buildScopePage(
	ctx context.Context,
	layout paths.Layout,
	scope tui.Scope,
	root string,
	cfg skillrepo.Config,
) (*statusScopePage, tui.StatusSection, error) {
	lib, err := skillrepo.NewLibrary(root, layout.SkinkHome, a.Git)
	if err != nil {
		return nil, tui.StatusSection{}, err
	}
	lib.Config = cfg
	if err := lib.EnsureCloned(ctx); err != nil {
		return nil, tui.StatusSection{}, err
	}

	skills, err := lib.ListAll()
	if err != nil {
		return nil, tui.StatusSection{}, err
	}

	title := "Project Skills"
	if scope == tui.ScopeGlobal {
		title = "Global Skills"
	}

	sp := &statusScopePage{
		root:       root,
		config:     cfg,
		repos:      map[string]statusRepoAction{},
		addedRepos: map[string]sourceSkillSelection{},
		skills:     map[string]statusSkillAction{},
	}
	sec := tui.StatusSection{Scope: scope, Title: title}

	if len(skills) == 0 {
		return sp, sec, nil
	}

	var targetRoot string
	if scope == tui.ScopeGlobal {
		targetRoot = layout.GlobalSkillDir(cfg.SkillDir)
	} else {
		targetRoot, err = a.resolveSkillDir(root, cfg)
		if err != nil {
			return nil, tui.StatusSection{}, err
		}
	}
	sp.targetRoot = targetRoot

	statuses, err := syncer.Check(syncItemsForSkills(skills), targetRoot)
	if err != nil {
		return nil, tui.StatusSection{}, err
	}
	statusByPath := map[string]syncer.Status{}
	for _, st := range statuses {
		statusByPath[st.Path] = st.Status
	}

	skillsByRepo := map[string][]skillrepo.Skill{}
	for _, skill := range skills {
		skillsByRepo[skill.Source] = append(skillsByRepo[skill.Source], skill)
	}

	for _, src := range lib.Sources {
		repoID := src.URL.DisplayPath()
		repoAction := statusRepoAction{source: src}
		repo := tui.StatusRepo{
			ID:       repoID,
			Scope:    scope,
			Name:     repoID,
			Version:  src.Version,
			Checking: true,
		}
		selection, selErr := sourceSkillSelectionFor(cfg, src.URL.Original, src.Repo)
		if selErr != nil {
			return nil, tui.StatusSection{}, selErr
		}
		if len(selection.discovered) == 0 {
			repo.BrowseError = fmt.Sprintf("no SKILL.md files found in %s", src.Repo.Dir)
		} else {
			repo.BrowseItems = selection.items
			repoAction.selection = selection
		}
		descriptionByDir := map[string]string{}
		for _, item := range selection.items {
			descriptionByDir[item.Path] = item.Description
		}
		for _, skill := range skillsByRepo[repoID] {
			dest := filepath.Join(targetRoot, skill.Name)
			status := statusByPath[dest]
			if status == "" {
				status = syncer.StatusDifferent
			}
			skillID := repoID + "|" + skill.SourceDir
			repo.Skills = append(repo.Skills, tui.StatusSkill{
				ID:          skillID,
				Name:        skill.Name,
				Path:        displayPath(root, dest),
				SourceDir:   skill.SourceDir,
				Description: descriptionByDir[skill.SourceDir],
				Status:      string(status),
			})
			sp.skills[skillID] = statusSkillAction{
				repoID:    repoID,
				name:      skill.Name,
				sourceDir: skill.SourceDir,
				sourceURL: skill.SourceURL,
				source:    skill.Path,
			}
		}
		sort.Slice(repo.Skills, func(i, j int) bool { return repo.Skills[i].Path < repo.Skills[j].Path })
		sp.repos[repoID] = repoAction
		sec.Repos = append(sec.Repos, repo)
	}
	sort.Slice(sec.Repos, func(i, j int) bool { return sec.Repos[i].Name < sec.Repos[j].Name })
	return sp, sec, nil
}

func statusUpdate(ctx context.Context, page statusPage) func() tui.StatusSnapshot {
	allRepos := page.snapshot.Repos()
	if len(allRepos) == 0 {
		return nil
	}
	// Collect all repo actions across scopes.
	allRepoActions := make(map[string]statusRepoAction)
	for _, sp := range page.scopes {
		for id, action := range sp.repos {
			allRepoActions[id] = action
		}
	}
	repoActions := cloneStatusRepoActions(allRepoActions)
	return func() tui.StatusSnapshot {
		page = checkStatusPageRepos(ctx, page, repoActions)
		return page.snapshot
	}
}

func cloneStatusRepoActions(in map[string]statusRepoAction) map[string]statusRepoAction {
	out := make(map[string]statusRepoAction, len(in))
	for id, action := range in {
		out[id] = action
	}
	return out
}

func checkStatusPageRepos(ctx context.Context, page statusPage, repoActions map[string]statusRepoAction) statusPage {
	for si := range page.snapshot.Sections {
		for ri := range page.snapshot.Sections[si].Repos {
			repoID := page.snapshot.Sections[si].Repos[ri].ID
			action, ok := repoActions[repoID]
			if !ok {
				continue
			}
			tags, upgrade, latestSemver, semverComparable, err := statusRepoTags(ctx, action.source)
			page.snapshot.Sections[si].Repos[ri].Checking = false
			if err != nil {
				page.snapshot.Sections[si].Repos[ri].Error = err.Error()
				continue
			}
			action.latestSemverTag = latestSemver
			action.semverComparable = semverComparable
			repoActions[repoID] = action
			page.snapshot.Sections[si].Repos[ri].Upgrade = upgrade
			page.snapshot.Sections[si].Repos[ri].Tags = statusTags(tags)
		}
	}
	return page
}

func statusRepoTags(ctx context.Context, src skillrepo.Source) ([]skillrepo.Tag, bool, string, bool, error) {
	if err := src.Repo.Fetch(ctx); err != nil {
		return nil, false, "", false, err
	}
	tags, err := src.Repo.Tags(ctx)
	if err != nil {
		return nil, false, "", false, err
	}
	remoteTags, err := src.Repo.RemoteTags(ctx)
	if err != nil {
		return nil, false, "", false, err
	}
	tags = skillrepo.MergeTags(tags, remoteTags)
	semverTags := skillrepo.SemverTags(tags)
	choices := selectableTags(tags, semverTags, src.Version)
	if src.Version == "" {
		changed, err := src.Repo.RemoteHeadChanged(ctx)
		if err != nil {
			return nil, false, "", false, err
		}
		return choices, changed, "", false, nil
	}
	newer, ok := skillrepo.NewerSemverTags(tags, src.Version)
	if ok {
		latest := ""
		if len(semverTags) > 0 {
			latest = semverTags[0].Name
		}
		return choices, len(newer) > 0, latest, true, nil
	}
	return choices, len(choices) > 0, "", false, nil
}

func selectableTags(tags, semverTags []skillrepo.Tag, current string) []skillrepo.Tag {
	choices := tags
	if len(semverTags) == len(tags) {
		choices = semverTags
	}
	if current == "" {
		return choices
	}
	out := make([]skillrepo.Tag, 0, len(choices))
	for _, tag := range choices {
		if tag.Name != current {
			out = append(out, tag)
		}
	}
	return out
}

func statusTags(tags []skillrepo.Tag) []tui.StatusTag {
	out := make([]tui.StatusTag, len(tags))
	for i, tag := range tags {
		out[i] = tui.StatusTag{Name: tag.Name, Created: tag.Created}
	}
	return out
}

func (a *App) statusAddRepo(ctx context.Context, page *statusPage) tui.StatusAddRepoFunc {
	return func(rawURL string) (tui.StatusAddRepoResult, error) {
		gitURL, repo, err := a.prepareRepo(ctx, page.skinkHome, rawURL)
		if err != nil {
			return tui.StatusAddRepoResult{}, err
		}
		// Use the first available scope page's config for existing import check.
		var cfg skillrepo.Config
		for _, sp := range page.scopes {
			cfg = sp.config
			break
		}
		selection, err := sourceSkillSelectionFor(cfg, gitURL.Original, repo)
		if err != nil {
			return tui.StatusAddRepoResult{}, err
		}
		if len(selection.discovered) == 0 {
			return tui.StatusAddRepoResult{}, fmt.Errorf("no SKILL.md files found in %s", repo.Dir)
		}
		// Store in all scope pages for lookup during apply.
		for _, sp := range page.scopes {
			sp.addedRepos[gitURL.Original] = selection
		}
		return tui.StatusAddRepoResult{URL: gitURL.Original, Items: selection.items}, nil
	}
}

func (a *App) handleStatusAction(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	switch action.Kind {
	case tui.StatusActionSync:
		return a.handleStatusSync(page, action)
	case tui.StatusActionDelete:
		return a.handleStatusDelete(page, action)
	case tui.StatusActionUpdateTag:
		return a.handleStatusUpdateTag(ctx, page, action)
	case tui.StatusActionNext:
		return a.handleStatusNext(ctx, page, action)
	case tui.StatusActionChooseSkills:
		return a.handleStatusChooseSkills(page, action)
	case tui.StatusActionAddRepo:
		return a.handleStatusAddRepo(ctx, page, action)
	default:
		return "", nil
	}
}

func (a *App) handleStatusSync(page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	skill, ok := sp.skills[action.SkillID]
	if !ok {
		return "", fmt.Errorf("unknown skill action %q", action.SkillID)
	}
	result, err := syncer.Sync([]syncer.Item{{Name: skill.name, Source: skill.source}}, sp.targetRoot, true)
	if err != nil {
		return "", err
	}
	if len(result.Conflicts) > 0 {
		return fmt.Sprintf("%s could not be synced", skill.name), nil
	}
	return fmt.Sprintf("synced %s", skill.name), nil
}

func (a *App) handleStatusDelete(page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	skill, ok := sp.skills[action.SkillID]
	if !ok {
		return "", fmt.Errorf("unknown skill action %q", action.SkillID)
	}
	cfg, err := skillrepo.RemoveRepoDir(sp.config, skill.sourceURL, skill.sourceDir)
	if err != nil {
		if errors.Is(err, skillrepo.ErrWildcardRemove) {
			return fmt.Sprintf("cannot delete %s: it is included by a wildcard import", skill.name), nil
		}
		return "", err
	}
	if err := skillrepo.SaveConfig(sp.root, cfg); err != nil {
		return "", err
	}
	if _, err := removeSkillDirs(sp.targetRoot, []string{skill.name}); err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted %s", skill.name), nil
}

func (a *App) handleStatusChooseSkills(page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	repo, ok := sp.repos[action.RepoID]
	if !ok {
		return "", fmt.Errorf("unknown repo action %q", action.RepoID)
	}
	if len(repo.selection.discovered) == 0 {
		return "", fmt.Errorf("no SKILL.md files found in %s", repo.source.Repo.Dir)
	}
	if err := a.applySourceSkillSelection(sp.root, sp.config, repo.source.URL.Original, sp.targetRoot, repo.selection, action.Selected); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated skills for %s", action.RepoID), nil
}

func (a *App) handleStatusAddRepo(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	if action.URL == "" {
		return "no repo URL entered", nil
	}
	selection, ok := sp.addedRepos[action.URL]
	if !ok {
		gitURL, repo, err := a.prepareRepo(ctx, page.skinkHome, action.URL)
		if err != nil {
			return "", err
		}
		selection, err = sourceSkillSelectionFor(sp.config, gitURL.Original, repo)
		if err != nil {
			return "", err
		}
		action.URL = gitURL.Original
	}
	if len(selection.discovered) == 0 {
		return "", fmt.Errorf("no SKILL.md files found in %s", action.URL)
	}
	if err := a.applySourceSkillSelection(sp.root, sp.config, action.URL, sp.targetRoot, selection, action.Selected); err != nil {
		return "", err
	}
	return fmt.Sprintf("added skills from %s", action.URL), nil
}

func (a *App) handleStatusUpdateTag(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	repo, ok := sp.repos[action.RepoID]
	if !ok {
		return "", fmt.Errorf("unknown repo action %q", action.RepoID)
	}
	if action.Tag == "" {
		return "no tag selected", nil
	}
	cfg := skillrepo.SetRepoVersion(sp.config, repo.source.URL.Original, action.Tag)
	if err := skillrepo.SaveConfig(sp.root, cfg); err != nil {
		return "", err
	}
	if err := repo.source.Repo.Fetch(ctx); err != nil {
		return "", err
	}
	if err := repo.source.Repo.Checkout(ctx, action.Tag); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated %s to %s", action.RepoID, action.Tag), nil
}

func (a *App) handleStatusNext(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	sp := page.scopePage(action.Scope)
	if sp == nil {
		return "", fmt.Errorf("no %s scope available", action.Scope)
	}
	repo, ok := sp.repos[action.RepoID]
	if !ok {
		return "", fmt.Errorf("unknown repo action %q", action.RepoID)
	}
	if repo.source.Version == "" {
		if err := repo.source.Repo.Pull(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("updated %s to HEAD", action.RepoID), nil
	}
	if action.Tag != "" {
		return a.handleStatusUpdateTag(ctx, page, tui.StatusAction{
			Kind:   tui.StatusActionUpdateTag,
			Scope:  action.Scope,
			RepoID: action.RepoID,
			Tag:    action.Tag,
		})
	}
	if !repo.semverComparable || repo.latestSemverTag == "" {
		_, _, latestSemver, semverComparable, err := statusRepoTags(ctx, repo.source)
		if err != nil {
			return "", err
		}
		repo.latestSemverTag = latestSemver
		repo.semverComparable = semverComparable
	}
	if !repo.semverComparable || repo.latestSemverTag == "" {
		return fmt.Sprintf("choose a tag for %s with t", action.RepoID), nil
	}
	if repo.latestSemverTag == repo.source.Version {
		return fmt.Sprintf("%s is already on newest tag", action.RepoID), nil
	}
	return a.handleStatusUpdateTag(ctx, page, tui.StatusAction{
		Kind:   tui.StatusActionUpdateTag,
		Scope:  action.Scope,
		RepoID: action.RepoID,
		Tag:    repo.latestSemverTag,
	})
}
