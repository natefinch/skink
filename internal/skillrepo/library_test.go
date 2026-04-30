package skillrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// libFakeGit is a minimal fake GitRunner for Library tests. It records
// every call and, when handling a `clone`, creates the target dir with a
// `.git` marker plus any top-level dirs listed in seedAfter for that clone
// dir. Other git commands just succeed.
type libFakeGit struct {
	seedAfter map[string][]string
	allCalls  [][]string
}

func (g *libFakeGit) Run(ctx context.Context, dir string, args ...string) error {
	call := append([]string{dir}, args...)
	g.allCalls = append(g.allCalls, call)
	if len(args) > 0 && args[0] == "clone" {
		target := filepath.Join(dir, args[len(args)-1])
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
			return err
		}
		for _, name := range g.seedAfter[target] {
			if err := os.MkdirAll(filepath.Join(target, name), 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedProject(t *testing.T, configBody, configName string) string {
	t.Helper()
	project := t.TempDir()
	if configName != "" {
		writeFile(t, project, configName, configBody)
	}
	return project
}

func TestNewLibraryRequiresProjectConfig(t *testing.T) {
	_, err := NewLibrary(t.TempDir(), t.TempDir(), &libFakeGit{})
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("want ErrConfigNotFound, got %v", err)
	}
}

func TestNewLibraryMergesImportsBySharedClone(t *testing.T) {
	cache := t.TempDir()
	project := seedProject(t, `
skilldir: /skills
imports:
  - url: github.com/anthropics/skills
    dirs:
      - skills/skill-creator
    version: v1
  - url: https://github.com/anthropics/skills.git
    dirs:
      - skills/pdf
    version: v1
  - url: git@github.com:me/other.git
`, ".skink.yaml")
	lib, err := NewLibrary(project, cache, &libFakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Sources) != 2 {
		t.Fatalf("want 2 sources, got %d: %+v", len(lib.Sources), lib.Sources)
	}
	if lib.Config.SkillDir != "skills" {
		t.Fatalf("library config SkillDir = %q", lib.Config.SkillDir)
	}
	if lib.Sources[0].URL.DisplayPath() != "github.com/anthropics/skills" {
		t.Errorf("source[0] = %+v", lib.Sources[0])
	}
	if len(lib.Sources[0].Imports) != 2 {
		t.Errorf("first source should have merged 2 imports, got %d", len(lib.Sources[0].Imports))
	}
	if lib.Sources[1].URL.DisplayPath() != "github.com/me/other" {
		t.Errorf("source[1] = %+v", lib.Sources[1])
	}
	wantDir := filepath.Join(cache, "github.com", "anthropics", "skills")
	if lib.Sources[0].Repo.Dir != wantDir {
		t.Errorf("clone dir = %q want %q", lib.Sources[0].Repo.Dir, wantDir)
	}
}

func TestNewLibraryVersionConflict(t *testing.T) {
	project := seedProject(t, `
imports:
  - url: github.com/anthropics/skills
    dirs:
      - skills/a
    version: v1
  - url: github.com/anthropics/skills
    dirs:
      - skills/b
    version: v2
`, ".skink.yaml")
	_, err := NewLibrary(project, t.TempDir(), &libFakeGit{})
	if err == nil || !strings.Contains(err.Error(), "conflicting versions") {
		t.Errorf("want conflict error, got %v", err)
	}
}

func TestEnsureClonedAndCheckout(t *testing.T) {
	cache := t.TempDir()
	project := seedProject(t, `
imports:
  - url: github.com/anthropics/skills
    dirs:
      - skills/skill-creator
    version: v1.2.3
  - url: github.com/anthropics/skills
    dirs:
      - skills/pdf
    version: v1.2.3
`, ".skink.yaml")
	g := &libFakeGit{}
	lib, err := NewLibrary(project, cache, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.EnsureCloned(context.Background()); err != nil {
		t.Fatal(err)
	}
	clones, checkouts := 0, 0
	for _, c := range g.allCalls {
		if len(c) >= 2 && c[1] == "clone" {
			clones++
		}
		if len(c) >= 3 && c[1] == "checkout" && c[2] == "v1.2.3" {
			checkouts++
		}
	}
	if clones != 1 {
		t.Errorf("want 1 clone, got %d (%v)", clones, g.allCalls)
	}
	if checkouts != 1 {
		t.Errorf("want 1 checkout, got %d", checkouts)
	}
}

func TestListAllExpandsDirSelectors(t *testing.T) {
	cache := t.TempDir()
	project := seedProject(t, `
imports:
  - url: github.com/anthropics/skills
    dirs:
      - skills/skill-creator
      - skills/*
  - url: git@example.com:my-org/my-repo
    # no dirs -> all top-level dirs
`, ".skink.yaml")

	anth := filepath.Join(cache, "github.com", "anthropics", "skills")
	must(t, os.MkdirAll(filepath.Join(anth, ".git"), 0o755))
	must(t, os.MkdirAll(filepath.Join(anth, "skills", "skill-creator"), 0o755))
	must(t, os.MkdirAll(filepath.Join(anth, "skills", "pdf"), 0o755))
	must(t, os.MkdirAll(filepath.Join(anth, "skills", "docx"), 0o755))

	mine := filepath.Join(cache, "example.com", "my-org", "my-repo")
	must(t, os.MkdirAll(filepath.Join(mine, ".git"), 0o755))
	must(t, os.MkdirAll(filepath.Join(mine, "alpha"), 0o755))
	must(t, os.MkdirAll(filepath.Join(mine, "beta"), 0o755))
	must(t, os.MkdirAll(filepath.Join(mine, ".hidden"), 0o755))

	lib, err := NewLibrary(project, cache, &libFakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	skills, err := lib.ListAll()
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, s := range skills {
		got[s.Name+"@"+s.Source] = s.InstallSubpath
	}

	want := map[string]string{
		"skill-creator@github.com/anthropics/skills": filepath.Join("github.com", "anthropics", "skills", "skills", "skill-creator"),
		"pdf@github.com/anthropics/skills":           filepath.Join("github.com", "anthropics", "skills", "skills", "pdf"),
		"docx@github.com/anthropics/skills":          filepath.Join("github.com", "anthropics", "skills", "skills", "docx"),
		"alpha@example.com/my-org/my-repo":           filepath.Join("example.com", "my-org", "my-repo", "alpha"),
		"beta@example.com/my-org/my-repo":            filepath.Join("example.com", "my-org", "my-repo", "beta"),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("skill %s: got %q want %q", k, got[k], v)
		}
	}
	for _, s := range skills {
		if s.Source == "" {
			t.Errorf("all skills should come from configured sources: %+v", s)
		}
		if s.Name == ".hidden" {
			t.Errorf(".hidden should be skipped at repo root: %+v", s)
		}
	}
}

func TestListAllMissingSubdirIsIgnored(t *testing.T) {
	cache := t.TempDir()
	project := seedProject(t, `
imports:
  - url: github.com/a/b
    dirs:
      - missing/skill
`, ".skink.yaml")
	anth := filepath.Join(cache, "github.com", "a", "b")
	must(t, os.MkdirAll(filepath.Join(anth, ".git"), 0o755))

	lib, err := NewLibrary(project, cache, &libFakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	skills, err := lib.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("want no skills, got %+v", skills)
	}
}

func TestPullAllPinnedAndUnpinned(t *testing.T) {
	cache := t.TempDir()
	project := seedProject(t, `
imports:
  - url: github.com/a/pinned
    version: v1
  - url: github.com/a/unpinned
`, ".skink.yaml")
	for _, p := range []string{"github.com/a/pinned", "github.com/a/unpinned"} {
		must(t, os.MkdirAll(filepath.Join(cache, p, ".git"), 0o755))
	}
	g := &libFakeGit{}
	lib, err := NewLibrary(project, cache, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.PullAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sawFetch, sawPullUnpinned, sawCheckout bool
	for _, c := range g.allCalls {
		if len(c) < 2 {
			continue
		}
		if strings.Contains(c[0], "pinned") && c[1] == "fetch" {
			sawFetch = true
		}
		if strings.Contains(c[0], "pinned") && c[1] == "checkout" {
			sawCheckout = true
		}
		if strings.Contains(c[0], "unpinned") && c[1] == "pull" {
			sawPullUnpinned = true
		}
	}
	if !sawFetch || !sawCheckout {
		t.Errorf("pinned repo should fetch+checkout: %v", g.allCalls)
	}
	if !sawPullUnpinned {
		t.Errorf("unpinned repo should pull: %v", g.allCalls)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
