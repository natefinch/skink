package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/natefinch/skink/internal/paths"
	"github.com/natefinch/skink/internal/skillrepo"
	"github.com/natefinch/skink/internal/syncer"
	"github.com/natefinch/skink/internal/tui"
)

type statusPage struct {
	snapshot    tui.StatusSnapshot
	skinkHome   string
	projectRoot string
	targetRoot  string
	config      skillrepo.Config
	repos       map[string]statusRepoAction
	addedRepos  map[string]sourceSkillSelection
	skills      map[string]statusSkillAction
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

func (a *App) runStatus(ctx context.Context) error {
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
	lib, projectRoot, err := a.projectLibrary(ctx)
	if err != nil {
		return statusPage{}, err
	}
	skills, err := lib.ListAll()
	if err != nil {
		return statusPage{}, err
	}
	if len(skills) == 0 {
		return statusPage{
			snapshot:    tui.StatusSnapshot{Message: message},
			skinkHome:   layout.SkinkHome,
			projectRoot: projectRoot,
			config:      lib.Config,
			repos:       map[string]statusRepoAction{},
			addedRepos:  map[string]sourceSkillSelection{},
			skills:      map[string]statusSkillAction{},
		}, nil
	}
	targetRoot, err := a.resolveSkillDir(projectRoot, lib.Config)
	if err != nil {
		return statusPage{}, err
	}
	statuses, err := syncer.Check(syncItemsForSkills(skills), targetRoot)
	if err != nil {
		return statusPage{}, err
	}
	statusByPath := map[string]syncer.Status{}
	for _, st := range statuses {
		statusByPath[st.Path] = st.Status
	}

	page := statusPage{
		skinkHome:   layout.SkinkHome,
		projectRoot: projectRoot,
		targetRoot:  targetRoot,
		config:      lib.Config,
		repos:       map[string]statusRepoAction{},
		addedRepos:  map[string]sourceSkillSelection{},
		skills:      map[string]statusSkillAction{},
	}
	page.snapshot.Message = message

	skillsByRepo := map[string][]skillrepo.Skill{}
	for _, skill := range skills {
		repoID := skill.Source
		skillsByRepo[repoID] = append(skillsByRepo[repoID], skill)
	}

	for _, src := range lib.Sources {
		repoID := src.URL.DisplayPath()
		repoAction := statusRepoAction{source: src}
		repo := tui.StatusRepo{
			ID:       repoID,
			Name:     repoID,
			Version:  src.Version,
			Checking: true,
		}
		selection, err := sourceSkillSelectionFor(lib.Config, src.URL.Original, src.Repo)
		if err != nil {
			return statusPage{}, err
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
				Path:        displayPath(projectRoot, dest),
				SourceDir:   skill.SourceDir,
				Description: descriptionByDir[skill.SourceDir],
				Status:      string(status),
			})
			page.skills[skillID] = statusSkillAction{
				repoID:    repoID,
				name:      skill.Name,
				sourceDir: skill.SourceDir,
				sourceURL: skill.SourceURL,
				source:    skill.Path,
			}
		}
		sort.Slice(repo.Skills, func(i, j int) bool { return repo.Skills[i].Path < repo.Skills[j].Path })
		page.repos[repoID] = repoAction
		page.snapshot.Repos = append(page.snapshot.Repos, repo)
	}
	sort.Slice(page.snapshot.Repos, func(i, j int) bool { return page.snapshot.Repos[i].Name < page.snapshot.Repos[j].Name })
	return page, nil
}

func statusUpdate(ctx context.Context, page statusPage) func() tui.StatusSnapshot {
	if len(page.snapshot.Repos) == 0 {
		return nil
	}
	page.repos = cloneStatusRepoActions(page.repos)
	return func() tui.StatusSnapshot {
		page = checkStatusPageRepos(ctx, page)
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

func checkStatusPageRepos(ctx context.Context, page statusPage) statusPage {
	for i := range page.snapshot.Repos {
		repoID := page.snapshot.Repos[i].ID
		action, ok := page.repos[repoID]
		if !ok {
			continue
		}
		tags, upgrade, latestSemver, semverComparable, err := statusRepoTags(ctx, action.source)
		page.snapshot.Repos[i].Checking = false
		if err != nil {
			page.snapshot.Repos[i].Error = err.Error()
			continue
		}
		action.latestSemverTag = latestSemver
		action.semverComparable = semverComparable
		page.repos[repoID] = action
		page.snapshot.Repos[i].Upgrade = upgrade
		page.snapshot.Repos[i].Tags = statusTags(tags)
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
		selection, err := sourceSkillSelectionFor(page.config, gitURL.Original, repo)
		if err != nil {
			return tui.StatusAddRepoResult{}, err
		}
		if len(selection.discovered) == 0 {
			return tui.StatusAddRepoResult{}, fmt.Errorf("no SKILL.md files found in %s", repo.Dir)
		}
		page.addedRepos[gitURL.Original] = selection
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
	skill, ok := page.skills[action.SkillID]
	if !ok {
		return "", fmt.Errorf("unknown skill action %q", action.SkillID)
	}
	result, err := syncer.Sync([]syncer.Item{{Name: skill.name, Source: skill.source}}, page.targetRoot, true)
	if err != nil {
		return "", err
	}
	if len(result.Conflicts) > 0 {
		return fmt.Sprintf("%s could not be synced", skill.name), nil
	}
	return fmt.Sprintf("synced %s", skill.name), nil
}

func (a *App) handleStatusDelete(page statusPage, action tui.StatusAction) (string, error) {
	skill, ok := page.skills[action.SkillID]
	if !ok {
		return "", fmt.Errorf("unknown skill action %q", action.SkillID)
	}
	cfg, err := skillrepo.RemoveRepoDir(page.config, skill.sourceURL, skill.sourceDir)
	if err != nil {
		if errors.Is(err, skillrepo.ErrWildcardRemove) {
			return fmt.Sprintf("cannot delete %s: it is included by a wildcard import", skill.name), nil
		}
		return "", err
	}
	if err := skillrepo.SaveConfig(page.projectRoot, cfg); err != nil {
		return "", err
	}
	if _, err := removeSkillDirs(page.targetRoot, []string{skill.name}); err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted %s", skill.name), nil
}

func (a *App) handleStatusChooseSkills(page statusPage, action tui.StatusAction) (string, error) {
	repo, ok := page.repos[action.RepoID]
	if !ok {
		return "", fmt.Errorf("unknown repo action %q", action.RepoID)
	}
	if len(repo.selection.discovered) == 0 {
		return "", fmt.Errorf("no SKILL.md files found in %s", repo.source.Repo.Dir)
	}
	if err := a.applySourceSkillSelection(page.projectRoot, page.config, repo.source.URL.Original, page.targetRoot, repo.selection, action.Selected); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated skills for %s", action.RepoID), nil
}

func (a *App) handleStatusAddRepo(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	if action.URL == "" {
		return "no repo URL entered", nil
	}
	selection, ok := page.addedRepos[action.URL]
	if !ok {
		gitURL, repo, err := a.prepareRepo(ctx, page.skinkHome, action.URL)
		if err != nil {
			return "", err
		}
		selection, err = sourceSkillSelectionFor(page.config, gitURL.Original, repo)
		if err != nil {
			return "", err
		}
		action.URL = gitURL.Original
	}
	if len(selection.discovered) == 0 {
		return "", fmt.Errorf("no SKILL.md files found in %s", action.URL)
	}
	if err := a.applySourceSkillSelection(page.projectRoot, page.config, action.URL, page.targetRoot, selection, action.Selected); err != nil {
		return "", err
	}
	return fmt.Sprintf("added skills from %s", action.URL), nil
}

func (a *App) handleStatusUpdateTag(ctx context.Context, page statusPage, action tui.StatusAction) (string, error) {
	repo, ok := page.repos[action.RepoID]
	if !ok {
		return "", fmt.Errorf("unknown repo action %q", action.RepoID)
	}
	if action.Tag == "" {
		return "no tag selected", nil
	}
	cfg := skillrepo.SetRepoVersion(page.config, repo.source.URL.Original, action.Tag)
	if err := skillrepo.SaveConfig(page.projectRoot, cfg); err != nil {
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
	repo, ok := page.repos[action.RepoID]
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
		RepoID: action.RepoID,
		Tag:    repo.latestSemverTag,
	})
}
