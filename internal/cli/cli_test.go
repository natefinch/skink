package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePrompter is a scripted Prompter for testing.
type fakePrompter struct {
	textAnswers   []string
	multiAnswers  [][]int
	singleAnswers []int
}

func (f *fakePrompter) Text(title, prompt, placeholder string) (string, error) {
	if len(f.textAnswers) == 0 {
		return "", fmt.Errorf("fakePrompter: no text answers left (asked: %q)", prompt)
	}
	ans := f.textAnswers[0]
	f.textAnswers = f.textAnswers[1:]
	return ans, nil
}
func (f *fakePrompter) MultiSelect(title string, items []string) ([]int, error) {
	if len(f.multiAnswers) == 0 {
		return nil, fmt.Errorf("fakePrompter: no multi answers left")
	}
	ans := f.multiAnswers[0]
	f.multiAnswers = f.multiAnswers[1:]
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

// fakeGit pretends a clone succeeded by creating target/.git.
type fakeGit struct {
	pullCalled bool
	pullDirs   []string
}

func (f *fakeGit) Run(_ context.Context, dir string, args ...string) error {
	if len(args) >= 3 && args[0] == "clone" {
		target := filepath.Join(dir, args[2])
		return os.MkdirAll(filepath.Join(target, ".git"), 0o755)
	}
	if len(args) >= 1 && args[0] == "pull" {
		f.pullCalled = true
		f.pullDirs = append(f.pullDirs, dir)
		return nil
	}
	return nil
}

// testEnv lets us override home and wd.
type testEnv struct{ home, wd string }

func (t testEnv) UserHomeDir() (string, error) { return t.home, nil }
func (t testEnv) Getwd() (string, error)       { return t.wd, nil }

// setup returns a ready-to-use App with a fake FS home and project root,
// plus the paths they resolve to.
func setup(t *testing.T) (*App, string, string, *fakePrompter, *fakeGit, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	proj := t.TempDir()
	p := &fakePrompter{}
	g := &fakeGit{}
	out := &bytes.Buffer{}
	app := &App{
		Env:      testEnv{home: home, wd: proj},
		Git:      g,
		Prompter: p,
		Out:      out,
		Err:      out,
	}
	return app, home, proj, p, g, out
}

// seedSkills populates a skills checkout with the given skill dirs and
// marks it as a git repo.
func seedSkills(t *testing.T, home string, skills ...string) string {
	t.Helper()
	checkout := filepath.Join(home, ".skink", "repo")
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if err := os.MkdirAll(filepath.Join(checkout, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return checkout
}

func seedConfig(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".skink")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "skills_repo: git@example:me/skills.git\ncheckout_dir: " + filepath.Join(dir, "repo") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, app *App, args ...string) error {
	t.Helper()
	cmd := app.Root()
	cmd.SetArgs(args)
	cmd.SetOut(app.Out)
	cmd.SetErr(app.Err)
	return cmd.Execute()
}

func TestInitPrompts(t *testing.T) {
	app, home, _, p, _, _ := setup(t)
	p.textAnswers = []string{"git@example:me/skills.git"}
	if err := run(t, app, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".skink", "config.yaml")); err != nil {
		t.Errorf("config not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".skink", "repo", ".git")); err != nil {
		t.Errorf("checkout not created: %v", err)
	}
}

func TestInitTwiceErrors(t *testing.T) {
	app, home, _, _, _, _ := setup(t)
	seedConfig(t, home)
	if err := run(t, app, "init"); err == nil {
		t.Error("second init should error")
	}
}

func TestInstallAutoDetect(t *testing.T) {
	app, home, proj, p, _, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha", "beta")
	// mark the project as a claude project
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	p.multiAnswers = [][]int{{0}} // pick "alpha" (sorted)
	if err := run(t, app, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	link := filepath.Join(proj, ".claude", "skills", "alpha")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink, got %s", info.Mode())
	}
}

func TestInstallPromptsForClientWhenAmbiguous(t *testing.T) {
	app, home, proj, p, _, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha")
	// ambiguous: both .claude and .github present
	for _, d := range []string{".claude", ".github"} {
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p.multiAnswers = [][]int{{0}}
	p.singleAnswers = []int{1} // pick "copilot" (index 1 of detected)
	if err := run(t, app, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	link := filepath.Join(proj, ".github", "skills", "alpha")
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("expected symlink at %s: %v", link, err)
	}
}

func TestInstallClientFlag(t *testing.T) {
	app, home, proj, _, _, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha", "beta")
	if err := run(t, app, "install", "--client=cursor", "--skill=beta"); err != nil {
		t.Fatalf("install: %v", err)
	}
	link := filepath.Join(proj, ".cursor", "skills", "beta")
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("expected symlink at %s: %v", link, err)
	}
}

func TestInstallUnknownSkill(t *testing.T) {
	app, home, _, _, _, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha")
	err := run(t, app, "install", "--client=claude", "--skill=nope")
	if err == nil || !strings.Contains(err.Error(), "unknown skill") {
		t.Errorf("want unknown skill error, got %v", err)
	}
}

func TestListShowsInstalledMarker(t *testing.T) {
	app, home, proj, _, _, out := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha", "beta")
	if err := run(t, app, "install", "--client=claude", "--skill=alpha"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(t, app, "list"); err != nil {
		t.Fatal(err)
	}
	txt := out.String()
	if !strings.Contains(txt, "✓ alpha") {
		t.Errorf("missing installed marker: %q", txt)
	}
	if !strings.Contains(txt, "claude") {
		t.Errorf("missing client name: %q", txt)
	}
	if strings.Contains(txt, "✓ beta") {
		t.Errorf("beta should not be marked installed: %q", txt)
	}
	_ = proj
}

func TestStatusEmpty(t *testing.T) {
	app, _, _, _, _, out := setup(t)
	if err := run(t, app, "status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no skills installed") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

func TestStatusAfterInstall(t *testing.T) {
	app, home, _, _, _, out := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha")
	if err := run(t, app, "install", "--client=claude", "--skill=alpha"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(t, app, "status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[claude]") || !strings.Contains(out.String(), "alpha") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

func TestUninstallRemovesSymlink(t *testing.T) {
	app, home, proj, _, _, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha")
	if err := run(t, app, "install", "--client=claude", "--skill=alpha"); err != nil {
		t.Fatal(err)
	}
	if err := run(t, app, "uninstall", "--client=claude", "--skill=alpha"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	link := filepath.Join(proj, ".claude", "skills", "alpha")
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("symlink should be gone: err=%v", err)
	}
	// source preserved
	if _, err := os.Stat(filepath.Join(home, ".skink", "repo", "alpha")); err != nil {
		t.Errorf("source removed: %v", err)
	}
}

func TestUpdateCallsPull(t *testing.T) {
	app, home, _, _, g, _ := setup(t)
	seedConfig(t, home)
	seedSkills(t, home, "alpha")
	if err := run(t, app, "update"); err != nil {
		t.Fatal(err)
	}
	if !g.pullCalled {
		t.Error("pull should have been called")
	}
}

func TestInstallFromImport(t *testing.T) {
	app, home, proj, _, _, _ := setup(t)
	seedConfig(t, home)
	checkout := seedSkills(t, home, "alpha")
	if err := os.WriteFile(filepath.Join(checkout, "skink.yaml"),
		[]byte("imports:\n  - url: git@example.com:team/skills.git\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-populate the import clone so EnsureCloned skips it.
	importDir := filepath.Join(home, ".skink", "example.com", "team", "skills")
	if err := os.MkdirAll(filepath.Join(importDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(importDir, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := run(t, app, "install", "--client=claude", "--skill=beta"); err != nil {
		t.Fatalf("install: %v", err)
	}
	link := filepath.Join(proj, ".claude", "skills", "example.com", "team", "skills", "beta")
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %s: %v", link, err)
	}
	if dest != filepath.Join(importDir, "beta") {
		t.Errorf("symlink points to %q, want %q", dest, filepath.Join(importDir, "beta"))
	}
}

func TestInstallClonesImportOnDemand(t *testing.T) {
	app, home, _, _, _, _ := setup(t)
	seedConfig(t, home)
	checkout := seedSkills(t, home, "alpha")
	if err := os.WriteFile(filepath.Join(checkout, "skink.yaml"),
		[]byte("imports:\n  - url: git@example.com:team/skills.git\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(t, app, "install", "--client=claude", "--skill=alpha"); err != nil {
		t.Fatalf("install: %v", err)
	}
	cloneDir := filepath.Join(home, ".skink", "example.com", "team", "skills", ".git")
	if _, err := os.Stat(cloneDir); err != nil {
		t.Errorf("import should have been cloned: %v", err)
	}
}

func TestUpdatePullsImports(t *testing.T) {
	app, home, _, _, g, _ := setup(t)
	seedConfig(t, home)
	checkout := seedSkills(t, home, "alpha")
	if err := os.WriteFile(filepath.Join(checkout, "skink.yaml"),
		[]byte("imports:\n  - url: github.com/acme/team\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	importDir := filepath.Join(home, ".skink", "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	g.pullDirs = nil
	if err := run(t, app, "update"); err != nil {
		t.Fatal(err)
	}
	if len(g.pullDirs) != 2 {
		t.Errorf("expected 2 pulls (primary + import), got %v", g.pullDirs)
	}
}

func TestListIncludesImportedSkills(t *testing.T) {
	app, home, _, _, _, out := setup(t)
	seedConfig(t, home)
	checkout := seedSkills(t, home, "alpha")
	if err := os.WriteFile(filepath.Join(checkout, "skink.yaml"),
		[]byte("imports:\n  - url: github.com/acme/team\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	importDir := filepath.Join(home, ".skink", "github.com", "acme", "team")
	if err := os.MkdirAll(filepath.Join(importDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(importDir, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(t, app, "list"); err != nil {
		t.Fatal(err)
	}
	txt := out.String()
	if !strings.Contains(txt, "alpha") || !strings.Contains(txt, "beta") {
		t.Errorf("missing skills in list: %q", txt)
	}
	if !strings.Contains(txt, "github.com/acme/team") {
		t.Errorf("missing import source marker: %q", txt)
	}
}

func TestFirstRunViaInstall(t *testing.T) {
	app, home, proj, p, _, _ := setup(t)
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Before install can pick a skill, ensureConfig will prompt.
	// Our fakeGit "clone" just makes .git — no skill dirs — so install will
	// surface "no skills found" after cloning. We verify the config was
	// written regardless.
	p.textAnswers = []string{"git@example:me/skills.git"}
	p.multiAnswers = [][]int{{0}}
	_ = run(t, app, "install")
	if _, err := os.Stat(filepath.Join(home, ".skink", "config.yaml")); err != nil {
		t.Errorf("config not written: %v", err)
	}
}
