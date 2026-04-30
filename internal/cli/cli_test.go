package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/natefinch/skink/internal/skillrepo"
	"github.com/natefinch/skink/internal/tui"
)

type fakeGit struct {
	cloneDirs    []string
	cloneArgs    [][]string
	pullDirs     []string
	fetchDirs    []string
	checkoutRefs []string
	seedAfter    map[string]map[string]string
	outputs      map[string]string
}

func (f *fakeGit) Run(_ context.Context, dir string, args ...string) error {
	if len(args) >= 3 && args[0] == "clone" {
		target := filepath.Join(dir, args[len(args)-1])
		f.cloneArgs = append(f.cloneArgs, append([]string(nil), args...))
		f.cloneDirs = append(f.cloneDirs, target)
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
			return err
		}
		for name, body := range f.seedAfter[target] {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(target, name)), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(target, name), []byte(body), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	if len(args) >= 1 && args[0] == "pull" {
		f.pullDirs = append(f.pullDirs, dir)
		return nil
	}
	if len(args) >= 1 && args[0] == "fetch" {
		f.fetchDirs = append(f.fetchDirs, dir)
		return nil
	}
	if len(args) >= 2 && args[0] == "checkout" {
		f.checkoutRefs = append(f.checkoutRefs, args[1])
		return nil
	}
	return nil
}

func (f *fakeGit) RunOutput(_ context.Context, dir string, args ...string) (string, error) {
	key := dir + "|" + strings.Join(args, " ")
	if f.outputs != nil {
		if out, ok := f.outputs[key]; ok {
			return out, nil
		}
	}
	switch strings.Join(args, " ") {
	case "for-each-ref --sort=-creatordate --format=%(refname:short)%00%(creatordate:iso8601-strict) refs/tags":
		return "", nil
	case "ls-remote --tags --refs origin":
		return "", nil
	case "rev-parse HEAD":
		return "local\n", nil
	case "ls-remote origin HEAD":
		return "local\tHEAD\n", nil
	default:
		return "", nil
	}
}

type fakePrompter struct {
	singleAnswers []int
	textAnswers   []string
	browseAnswers [][]int
	browseErrors  []error
	browseItems   []tui.BrowseItem
	statusActions []tui.StatusAction
	statusInitial []tui.StatusSnapshot
	statusItems   []tui.StatusSnapshot
	statusApplies int
	onStatusStart func(tui.StatusSnapshot)
}

func (f *fakePrompter) Text(title, prompt, placeholder string) (string, error) {
	if len(f.textAnswers) == 0 {
		return "", fmt.Errorf("fakePrompter: no text answers left")
	}
	ans := f.textAnswers[0]
	f.textAnswers = f.textAnswers[1:]
	return ans, nil
}

func (f *fakePrompter) SingleSelect(title string, items []string) (int, error) {
	if len(f.singleAnswers) == 0 {
		return 0, fmt.Errorf("fakePrompter: no single answers left")
	}
	ans := f.singleAnswers[0]
	f.singleAnswers = f.singleAnswers[1:]
	return ans, nil
}

func (f *fakePrompter) BrowseSkills(title string, items []tui.BrowseItem) ([]int, error) {
	f.browseItems = append([]tui.BrowseItem(nil), items...)
	if len(f.browseErrors) > 0 {
		err := f.browseErrors[0]
		f.browseErrors = f.browseErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.browseAnswers) == 0 {
		return nil, fmt.Errorf("fakePrompter: no browse answers left")
	}
	ans := f.browseAnswers[0]
	f.browseAnswers = f.browseAnswers[1:]
	return ans, nil
}

func (f *fakePrompter) Status(title string, snapshot tui.StatusSnapshot, update func() tui.StatusSnapshot, addRepo tui.StatusAddRepoFunc) (tui.StatusAction, error) {
	f.statusInitial = append(f.statusInitial, snapshot)
	if f.onStatusStart != nil {
		f.onStatusStart(snapshot)
	}
	if update != nil {
		snapshot = update()
	}
	f.statusItems = append(f.statusItems, snapshot)
	if len(f.statusActions) == 0 {
		return tui.StatusAction{Kind: tui.StatusActionQuit}, nil
	}
	ans := f.statusActions[0]
	f.statusActions = f.statusActions[1:]
	return ans, nil
}

func (f *fakePrompter) InteractiveStatus(
	title string,
	snapshot tui.StatusSnapshot,
	update func() tui.StatusSnapshot,
	addRepo tui.StatusAddRepoFunc,
	apply tui.StatusApplyFunc,
) error {
	f.statusInitial = append(f.statusInitial, snapshot)
	if f.onStatusStart != nil {
		f.onStatusStart(snapshot)
	}
	if update != nil {
		snapshot = update()
	}
	f.statusItems = append(f.statusItems, snapshot)
	for len(f.statusActions) > 0 {
		action := f.statusActions[0]
		f.statusActions = f.statusActions[1:]
		if action.Kind == "" || action.Kind == tui.StatusActionQuit {
			return nil
		}
		next, err := apply(action)
		if err != nil {
			return err
		}
		f.statusApplies++
		f.statusItems = append(f.statusItems, next)
	}
	return nil
}

type testEnv struct{ home, wd string }

func (t testEnv) UserHomeDir() (string, error) { return t.home, nil }
func (t testEnv) Getwd() (string, error)       { return t.wd, nil }

func setup(t *testing.T) (*App, string, string, *fakeGit, *fakePrompter, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	proj := t.TempDir()
	g := &fakeGit{seedAfter: map[string]map[string]string{}}
	p := &fakePrompter{}
	out := &bytes.Buffer{}
	app := &App{
		Env:      testEnv{home: home, wd: proj},
		Git:      g,
		Prompter: p,
		Out:      out,
		Err:      out,
	}
	return app, home, proj, g, p, out
}

func run(t *testing.T, app *App, args ...string) error {
	t.Helper()
	cmd := app.Root()
	cmd.SetArgs(args)
	cmd.SetOut(app.Out)
	cmd.SetErr(app.Err)
	return cmd.Execute()
}

func writeProjectConfig(t *testing.T, projectRoot, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, ".skink.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedImport(t *testing.T, home string, elems ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{home, ".skink"}, elems...)...)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func gitOutputKey(dir string, args ...string) string {
	return dir + "|" + strings.Join(args, " ")
}

func tagOutputKey(dir string) string {
	return gitOutputKey(dir, "for-each-ref", "--sort=-creatordate", "--format=%(refname:short)%00%(creatordate:iso8601-strict)", "refs/tags")
}

func remoteTagOutputKey(dir string) string {
	return gitOutputKey(dir, "ls-remote", "--tags", "--refs", "origin")
}

func TestRootRegistersCurrentCommands(t *testing.T) {
	app, _, _, _, _, _ := setup(t)
	root := app.Root()
	commands := map[string]bool{}
	for _, cmd := range root.Commands() {
		commands[cmd.Name()] = true
	}
	for _, name := range []string{"sync"} {
		if !commands[name] {
			t.Fatalf("missing command %q", name)
		}
	}
	for _, name := range []string{"browse", "completion", "init", "install", "list", "status", "uninstall", "update"} {
		if commands[name] {
			t.Fatalf("obsolete command %q is still registered", name)
		}
	}
}

func TestStatusShowsGroupedSkillDirectories(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := os.MkdirAll(filepath.Join(importDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(importDir, name, "SKILL.md"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "beta", "SKILL.md"), []byte("local beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(p.statusItems) != 1 {
		t.Fatalf("status snapshots = %d", len(p.statusItems))
	}
	snap := p.statusItems[0]
	if len(snap.Repos) != 1 || snap.Repos[0].Name != "github.com/acme/team" {
		t.Fatalf("unexpected repos: %+v", snap.Repos)
	}
	got := snap.Repos[0].Skills
	if len(got) != 3 {
		t.Fatalf("skills = %+v", got)
	}
	want := map[string]string{
		"skills/alpha": "up to date",
		"skills/beta":  "different",
		"skills/gamma": "missing",
	}
	for _, skill := range got {
		if want[skill.Path] != skill.Status {
			t.Fatalf("skill status for %s = %q want %q; all=%+v", skill.Path, skill.Status, want[skill.Path], got)
		}
	}
}

func TestStatusStartsBeforeRemoteRepoChecks(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
    version: v1.0.0
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		tagOutputKey(importDir): "v1.1.0\x002026-01-02T00:00:00Z\nv1.0.0\x002026-01-01T00:00:00Z\n",
	}
	p.statusActions = []tui.StatusAction{{Kind: tui.StatusActionQuit}}
	p.onStatusStart = func(snapshot tui.StatusSnapshot) {
		if len(g.fetchDirs) != 0 {
			t.Fatalf("remote checks started before initial status render: %v", g.fetchDirs)
		}
		if len(snapshot.Repos) != 1 || !snapshot.Repos[0].Checking {
			t.Fatalf("initial status should mark repo as checking: %+v", snapshot.Repos)
		}
		if snapshot.Repos[0].Upgrade || len(snapshot.Repos[0].Tags) != 0 {
			t.Fatalf("initial status should not wait for remote tag data: %+v", snapshot.Repos[0])
		}
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(p.statusItems) != 1 || p.statusItems[0].Repos[0].Checking || !p.statusItems[0].Repos[0].Upgrade {
		t.Fatalf("updated status should include remote check result: %+v", p.statusItems)
	}
}

func TestStatusSyncActionOverwritesOneSkillAndRefreshes(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "alpha", "SKILL.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionSync, RepoID: "github.com/acme/team", SkillID: "github.com/acme/team|alpha"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(proj, "skills", "alpha", "SKILL.md")); err != nil || string(got) != "alpha\n" {
		t.Fatalf("expected overwritten synced skill, got %q err=%v", got, err)
	}
	if len(p.statusItems) != 2 {
		t.Fatalf("status should refresh after sync, snapshots=%d", len(p.statusItems))
	}
	if p.statusItems[1].Repos[0].Skills[0].Status != "up to date" {
		t.Fatalf("refreshed status = %+v", p.statusItems[1].Repos[0].Skills)
	}
}

func TestStatusDeleteActionRemovesSkillAndConfig(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionDelete, RepoID: "github.com/acme/team", SkillID: "github.com/acme/team|alpha"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(p.statusItems) != 2 || len(p.statusItems[1].Repos) != 0 {
		t.Fatalf("status should refresh to empty page after delete: %+v", p.statusItems)
	}
	if _, err := os.Stat(filepath.Join(proj, "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("local skill should be removed, err=%v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(proj, ".skink.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "github.com/acme/team") || strings.Contains(string(cfg), "alpha") {
		t.Fatalf("config should remove last repo import:\n%s", cfg)
	}
}

func TestStatusDeleteActionBlocksWildcardImport(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - skills/*
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "skills", "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionDelete, RepoID: "github.com/acme/team", SkillID: "github.com/acme/team|skills/alpha"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("wildcard delete should not remove local skill: %v", err)
	}
	if len(p.statusItems) != 2 || !strings.Contains(p.statusItems[1].Message, "wildcard") {
		t.Fatalf("expected wildcard message after refresh: %+v", p.statusItems)
	}
}

func TestStatusChooseSkillsActionBrowsesRepo(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(importDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\nname: %s\ndescription: %s.\n---\n", name, name)
		if err := os.WriteFile(filepath.Join(importDir, name, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionChooseSkills, RepoID: "github.com/acme/team", Selected: []int{1, 2}},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	items := p.statusItems[0].Repos[0].BrowseItems
	if len(items) != 3 || items[0].Name != "All skills" || items[0].Path != "*" || !items[1].Selected || items[2].Selected {
		t.Fatalf("expected existing skill prechecked in status browse data: %+v", items)
	}
	skills := p.statusItems[0].Repos[0].Skills
	if len(skills) != 1 || skills[0].Description != "alpha." {
		t.Fatalf("expected status skill description from repo metadata: %+v", skills)
	}
	cfg, err := os.ReadFile(filepath.Join(proj, ".skink.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	txt := string(cfg)
	if !strings.Contains(txt, "- alpha") || !strings.Contains(txt, "- beta") {
		t.Fatalf("config should include selected skills:\n%s", txt)
	}
	if _, err := os.Stat(filepath.Join(proj, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("expected selected skill to sync: %v", err)
	}
}

func TestStatusChooseSkillsAllOptionWritesWildcard(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - packs/alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(importDir, "packs", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\nname: %s\ndescription: %s.\n---\n", name, name)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionChooseSkills, RepoID: "github.com/acme/team", Selected: []int{0}},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	items := p.statusItems[0].Repos[0].BrowseItems
	if len(items) != 3 || items[0].Name != "All skills" || items[0].Path != "packs/*" {
		t.Fatalf("expected all-skills wildcard row first: %+v", items)
	}
	cfg, err := skillrepo.ReadImports(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Imports) != 1 || strings.Join(cfg.Imports[0].Dirs, ",") != "packs/*" {
		t.Fatalf("config should use wildcard selector: %+v", cfg.Imports)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(proj, "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("expected %s to sync from wildcard selection: %v", name, err)
		}
	}
}

func TestStatusAddRepoActionPromptsAndBrowsesRepo(t *testing.T) {
	app, home, proj, g, p, out := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newClone := filepath.Join(home, ".skink", "github.com", "acme", "new")
	g.seedAfter[newClone] = map[string]string{
		"beta/SKILL.md": "---\nname: beta\ndescription: Beta.\n---\n",
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionAddRepo, URL: "github.com/acme/new", Selected: []int{0}},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(g.cloneArgs) != 1 || strings.Join(g.cloneArgs[0], " ") != "clone --depth=1 https://github.com/acme/new.git new" {
		t.Fatalf("clone args = %v", g.cloneArgs)
	}
	cfg, err := os.ReadFile(filepath.Join(proj, ".skink.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	txt := string(cfg)
	if !strings.Contains(txt, "url: github.com/acme/team") || !strings.Contains(txt, "url: github.com/acme/new") {
		t.Fatalf("config should include existing and added repos:\n%s", txt)
	}
	if _, err := os.Stat(filepath.Join(proj, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("expected selected skill to sync: %v", err)
	}
	if p.statusApplies != 1 {
		t.Fatalf("expected add repo to apply inside one status TUI session, got %d applies", p.statusApplies)
	}
	if out.Len() != 0 {
		t.Fatalf("status add repo should not print outside the TUI:\n%s", out.String())
	}
}

func TestStatusUpdateTagActionPinsAndChecksOut(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
    version: v1.0.0
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		tagOutputKey(importDir): "v1.1.0\x002026-01-02T00:00:00Z\nv1.0.0\x002026-01-01T00:00:00Z\n",
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionUpdateTag, RepoID: "github.com/acme/team", Tag: "v1.1.0"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(proj, ".skink.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "version: v1.1.0") {
		t.Fatalf("config not updated:\n%s", cfg)
	}
	if len(g.checkoutRefs) == 0 || g.checkoutRefs[len(g.checkoutRefs)-1] != "v1.1.0" {
		t.Fatalf("checkout refs = %v", g.checkoutRefs)
	}
}

func TestStatusUsesRemoteTagsWhenLocalTagsMissing(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
    version: v1.0.0
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		tagOutputKey(importDir):       "",
		remoteTagOutputKey(importDir): "aaa\trefs/tags/v1.0.0\nbbb\trefs/tags/v1.1.0\n",
	}
	p.statusActions = []tui.StatusAction{{Kind: tui.StatusActionQuit}}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(p.statusItems) != 1 || len(p.statusItems[0].Repos) != 1 {
		t.Fatalf("status snapshots = %+v", p.statusItems)
	}
	repo := p.statusItems[0].Repos[0]
	if !repo.Upgrade {
		t.Fatalf("repo should show update from remote tags: %+v", repo)
	}
	if len(repo.Tags) != 1 || repo.Tags[0].Name != "v1.1.0" {
		t.Fatalf("repo tag picker choices = %+v", repo.Tags)
	}
}

func TestStatusShowsOtherTagsWhenPinnedToNewestSemver(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
    version: v1.2.0
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		tagOutputKey(importDir): "v1.2.0\x002026-01-03T00:00:00Z\nv1.1.0\x002026-01-02T00:00:00Z\nv1.0.0\x002026-01-01T00:00:00Z\n",
	}
	p.statusActions = []tui.StatusAction{{Kind: tui.StatusActionQuit}}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(p.statusItems) != 1 || len(p.statusItems[0].Repos) != 1 {
		t.Fatalf("status snapshots = %+v", p.statusItems)
	}
	repo := p.statusItems[0].Repos[0]
	if repo.Upgrade {
		t.Fatalf("repo should not show an update when already on newest tag: %+v", repo)
	}
	want := []string{"v1.1.0", "v1.0.0"}
	if len(repo.Tags) != len(want) {
		t.Fatalf("repo tag picker choices = %+v", repo.Tags)
	}
	for i, tag := range repo.Tags {
		if tag.Name != want[i] {
			t.Fatalf("repo tag picker choice %d = %q want %q; all=%+v", i, tag.Name, want[i], repo.Tags)
		}
	}
}

func TestStatusNextActionUpdatesPinnedToNewestSemver(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
    version: v1.0.0
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		tagOutputKey(importDir): "v1.2.0\x002026-01-03T00:00:00Z\nv1.1.0\x002026-01-02T00:00:00Z\nv1.0.0\x002026-01-01T00:00:00Z\n",
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionNext, RepoID: "github.com/acme/team"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(g.checkoutRefs) == 0 || g.checkoutRefs[len(g.checkoutRefs)-1] != "v1.2.0" {
		t.Fatalf("checkout refs = %v", g.checkoutRefs)
	}
}

func TestStatusNextActionPullsUnpinnedHead(t *testing.T) {
	app, home, proj, g, p, _ := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.outputs = map[string]string{
		gitOutputKey(importDir, "rev-parse", "HEAD"):           "local\n",
		gitOutputKey(importDir, "ls-remote", "origin", "HEAD"): "remote\tHEAD\n",
	}
	p.statusActions = []tui.StatusAction{
		{Kind: tui.StatusActionNext, RepoID: "github.com/acme/team"},
		{Kind: tui.StatusActionQuit},
	}

	if err := run(t, app); err != nil {
		t.Fatal(err)
	}
	if len(g.pullDirs) == 0 || g.pullDirs[len(g.pullDirs)-1] != importDir {
		t.Fatalf("pull dirs = %v", g.pullDirs)
	}
	if len(p.statusItems) == 0 || !p.statusItems[0].Repos[0].Upgrade {
		t.Fatalf("expected upgrade indicator for changed remote head: %+v", p.statusItems)
	}
}

func TestUnknownOldCommandErrors(t *testing.T) {
	app, _, _, _, _, _ := setup(t)
	err := run(t, app, "install")
	if err == nil || !strings.Contains(fmt.Sprint(err), "unknown command") {
		t.Fatalf("want unknown command error, got %v", err)
	}
}

func TestSyncUsesConfiguredSkillDir(t *testing.T) {
	app, home, proj, g, _, out := setup(t)
	writeProjectConfig(t, proj, `
skilldir: /skills
imports:
  - url: github.com/acme/team
    dirs:
      - nested/alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "nested", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "nested", "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app, "sync"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(proj, "skills", "alpha", "SKILL.md")); err != nil || string(got) != "alpha\n" {
		t.Fatalf("copied skill = %q err=%v", got, err)
	}
	if len(g.pullDirs) != 1 {
		t.Fatalf("sync should update configured source, pullDirs=%v", g.pullDirs)
	}
	if !strings.Contains(out.String(), "added:") || !strings.Contains(out.String(), filepath.Join("skills", "alpha")) {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestSyncAutodetectsSingleClient(t *testing.T) {
	app, home, proj, _, _, _ := setup(t)
	writeProjectConfig(t, proj, `
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	if err := os.MkdirAll(filepath.Join(proj, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app, "sync"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".github", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected copied skill in autodetected copilot dir: %v", err)
	}
}

func TestSyncPromptsWhenNoClientDetected(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	p.singleAnswers = []int{1} // copilot
	writeProjectConfig(t, proj, `
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app, "sync"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".github", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected copied skill in prompted copilot dir: %v", err)
	}
}

func TestSyncPromptsWhenMultipleClientsDetected(t *testing.T) {
	app, home, proj, _, p, _ := setup(t)
	p.singleAnswers = []int{1} // copilot among detected claude/copilot
	writeProjectConfig(t, proj, `
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	for _, dir := range []string{".claude", ".github"} {
		if err := os.MkdirAll(filepath.Join(proj, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app, "sync"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".github", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected copied skill in prompted copilot dir: %v", err)
	}
}

func TestSyncConflictAndOverwrite(t *testing.T) {
	app, home, proj, _, _, out := setup(t)
	writeProjectConfig(t, proj, `
skilldir: skills
imports:
  - url: github.com/acme/team
    dirs:
      - alpha
`)
	importDir := seedImport(t, home, "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "alpha", "SKILL.md"), []byte("cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "skills", "alpha", "SKILL.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run(t, app, "sync")
	if err == nil || !strings.Contains(err.Error(), "sync conflicts") {
		t.Fatalf("want conflict error, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "skills", "alpha", "SKILL.md")); string(got) != "local\n" {
		t.Fatalf("conflict should preserve local file, got %q", got)
	}

	out.Reset()
	if err := run(t, app, "sync", "-f"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "skills", "alpha", "SKILL.md")); string(got) != "cache\n" {
		t.Fatalf("-f should overwrite local file, got %q", got)
	}
	if !strings.Contains(out.String(), "overwritten:") {
		t.Fatalf("unexpected overwrite output: %q", out.String())
	}
}
