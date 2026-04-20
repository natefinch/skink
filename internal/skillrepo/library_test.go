package skillrepo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// libFakeGit simulates clone by creating .git and any skill dirs the test
// pre-seeds via seedDir (keyed by the dir the clone will land in).
type libFakeGit struct {
	clonesAt  []string
	pullsAt   []string
	pullErr   map[string]error
	seedAfter map[string][]string // target dir -> list of skill dir names to create
}

func (f *libFakeGit) Run(_ context.Context, dir string, args ...string) error {
	if len(args) >= 3 && args[0] == "clone" {
		target := filepath.Join(dir, args[2])
		f.clonesAt = append(f.clonesAt, target)
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
			return err
		}
		for _, s := range f.seedAfter[target] {
			if err := os.MkdirAll(filepath.Join(target, s), 0o755); err != nil {
				return err
			}
		}
		return nil
	}
	if len(args) >= 1 && args[0] == "pull" {
		f.pullsAt = append(f.pullsAt, dir)
		if err, ok := f.pullErr[dir]; ok {
			return err
		}
	}
	return nil
}

func seedPrimary(t *testing.T, home string, skills []string, configBody, configName string) string {
	t.Helper()
	primary := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(primary, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if err := os.MkdirAll(filepath.Join(primary, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if configBody != "" {
		if err := os.WriteFile(filepath.Join(primary, configName), []byte(configBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return primary
}

func TestLibraryNoConfig(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"a", "b"}, "", "")
	lib, err := NewLibrary(home, &libFakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Imports) != 0 {
		t.Errorf("imports = %+v", lib.Imports)
	}
	skills, err := lib.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Errorf("got %+v", skills)
	}
	for _, s := range skills {
		if s.Source != "" {
			t.Errorf("primary skill source should be empty, got %q", s.Source)
		}
	}
}

func TestLibraryWithImports(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"alpha"}, `
imports:
  - name: team
    url: git@example:team/skills.git
  - url: https://github.com/org/more.git
`, "skillnk.yaml")

	g := &libFakeGit{
		seedAfter: map[string][]string{
			filepath.Join(home, "team"):     {"beta", "gamma"},
			filepath.Join(home, "org/more"): {"delta"},
		},
	}
	lib, err := NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Imports) != 2 {
		t.Fatalf("imports = %+v", lib.Imports)
	}
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if len(g.clonesAt) != 2 {
		t.Errorf("clones = %v", g.clonesAt)
	}
	// Calling again should be a no-op (both exist).
	g.clonesAt = nil
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if len(g.clonesAt) != 0 {
		t.Errorf("second call should clone nothing, got %v", g.clonesAt)
	}

	// ListAll should return skills from all sources, tagged with source.
	skills, err := lib.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"alpha": "",
		"beta":  "team",
		"gamma": "team",
		"delta": "org/more",
	}
	if len(skills) != len(want) {
		t.Fatalf("got %+v", skills)
	}
	for _, s := range skills {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected skill %q", s.Name)
			continue
		}
		if s.Source != w {
			t.Errorf("skill %q source = %q want %q", s.Name, s.Source, w)
		}
	}
}

func TestLibraryDedupePrimaryWins(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"shared"}, `
imports:
  - name: team
    url: x
`, "skillnk.yaml")
	g := &libFakeGit{
		seedAfter: map[string][]string{
			filepath.Join(home, "team"): {"shared", "teamonly"},
		},
	}
	lib, err := NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	skills, err := lib.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %+v", skills)
	}
	// "shared" must come from primary.
	for _, s := range skills {
		if s.Name == "shared" && s.Source != "" {
			t.Errorf("shared should be primary, got source %q", s.Source)
		}
	}
}

func TestLibraryDoesNotFollowTransitiveImports(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"p1"}, `
imports:
  - name: team
    url: x
`, "skillnk.yaml")
	teamDir := filepath.Join(home, "team")
	g := &libFakeGit{
		seedAfter: map[string][]string{
			teamDir: {"t1"},
		},
	}
	lib, err := NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	// Now add a transitive config to the team repo claiming further imports.
	if err := os.WriteFile(filepath.Join(teamDir, "skillnk.yaml"),
		[]byte("imports:\n - url: https://github.com/should/not/be/followed\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	// Rebuild library; should still only have one import.
	lib, err = NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Imports) != 1 || lib.Imports[0].Name != "team" {
		t.Errorf("transitive imports followed: %+v", lib.Imports)
	}
	// And cloning is a no-op.
	g.clonesAt = nil
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if len(g.clonesAt) != 0 {
		t.Errorf("should not clone transitive import: %v", g.clonesAt)
	}
}

func TestLibraryPullAll(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"p"}, `
imports:
  - name: team
    url: x
`, "skillnk.yaml")
	g := &libFakeGit{
		seedAfter: map[string][]string{filepath.Join(home, "team"): {"t"}},
	}
	lib, err := NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if err := lib.PullAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.pullsAt) != 2 {
		t.Errorf("pulls = %v (want primary + team)", g.pullsAt)
	}
}

func TestLibraryPullAllCollectsErrors(t *testing.T) {
	home := t.TempDir()
	seedPrimary(t, home, []string{"p"}, `
imports:
  - name: team
    url: x
`, "skillnk.yaml")
	teamDir := filepath.Join(home, "team")
	g := &libFakeGit{
		seedAfter: map[string][]string{teamDir: {"t"}},
	}
	lib, err := NewLibrary(home, g)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.EnsureImportsCloned(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	// Set import pull to fail.
	g.pullErr = map[string]error{teamDir: errTest("boom")}
	err = lib.PullAll(context.Background())
	if err == nil {
		t.Error("expected error")
	}
	// primary should still have been pulled
	if len(g.pullsAt) < 2 {
		t.Errorf("pulls = %v, want primary attempted too", g.pullsAt)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
