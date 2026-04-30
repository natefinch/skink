package skillrepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadImportsNoFile(t *testing.T) {
	_, err := ReadImports(t.TempDir())
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("want ErrConfigNotFound, got %v", err)
	}
}

func TestReadImportsAllFormats(t *testing.T) {
	cases := []struct {
		fname, body, skillDir string
	}{
		{".skink.yaml", `
skilldir: /skills
imports:
  - url: git@github.com:acme/team-skills.git
    dirs:
      - skills/foo
      - skills/bar/*
    version: v1
  - url: https://github.com/charm/skills.git
`, "skills"},
		{".skink.json", `{"imports":[
  {"url":"git@github.com:acme/team-skills.git","dirs":["skills/foo","skills/bar/*"],"version":"v1"},
  {"url":"https://github.com/charm/skills.git"}
],"skilldir":"/skills"}`, "skills"},
		{".skink.toml", `
skilldir = "/skills"

[[imports]]
url = "git@github.com:acme/team-skills.git"
dirs = ["skills/foo", "skills/bar/*"]
version = "v1"

[[imports]]
url = "https://github.com/charm/skills.git"
`, "skills"},
	}
	for _, c := range cases {
		t.Run(c.fname, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, c.fname, c.body)
			got, err := ReadImports(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Imports) != 2 {
				t.Fatalf("got %+v", got)
			}
			if got.SkillDir != c.skillDir {
				t.Errorf("SkillDir = %q want %q", got.SkillDir, c.skillDir)
			}
			if got.Imports[0].URL != "git@github.com:acme/team-skills.git" || strings.Join(got.Imports[0].Dirs, ",") != "skills/foo,skills/bar/*" || got.Imports[0].Version != "v1" {
				t.Errorf("got.Imports[0] = %+v", got.Imports[0])
			}
			if got.Imports[1].URL != "https://github.com/charm/skills.git" {
				t.Errorf("got.Imports[1] = %+v", got.Imports[1])
			}
		})
	}
}

func TestNormalizeSkillDir(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"", "", false},
		{"skills", "skills", false},
		{"/skills", "skills", false},
		{" /skills/team ", "skills/team", false},
		{"/", "", true},
		{"../skills", "", true},
		{"skills/../other", "", true},
		{"./skills", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeSkillDir(c.in)
		if c.err {
			if err == nil {
				t.Errorf("NormalizeSkillDir(%q) want error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeSkillDir(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeSkillDir(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestReadImportsBadSkillDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".skink.yaml", "skilldir: ../escape\nimports: []\n")
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "skilldir") {
		t.Errorf("want skilldir error, got %v", err)
	}
}

func TestSaveConfigCreatesYAMLAndAddImportDirsMerges(t *testing.T) {
	dir := t.TempDir()
	cfg := AddImportDirs(Config{SkillDir: "skills"}, "github.com/acme/skills", []string{"old"})
	cfg = AddImportDirs(cfg, "github.com/acme/skills", []string{"old", "new"})
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.SkillDir != "skills" || len(got.Imports) != 1 {
		t.Fatalf("unexpected config: %+v", got)
	}
	dirs := strings.Join(got.Imports[0].Dirs, ",")
	if dirs != "old,new" {
		t.Fatalf("dirs = %q", dirs)
	}
}

func TestSetRepoVersionUpdatesMatchingImports(t *testing.T) {
	cfg := Config{Imports: []Import{
		{URL: "github.com/acme/skills", Dirs: []string{"one"}, Version: "v1.0.0"},
		{URL: "https://github.com/acme/skills.git", Dirs: []string{"two"}, Version: "v1.0.0"},
		{URL: "github.com/other/skills", Dirs: []string{"three"}, Version: "v1.0.0"},
	}}
	got := SetRepoVersion(cfg, "github.com/acme/skills", "v2.0.0")
	if got.Imports[0].Version != "v2.0.0" || got.Imports[1].Version != "v2.0.0" {
		t.Fatalf("matching imports not updated: %+v", got.Imports)
	}
	if got.Imports[2].Version != "v1.0.0" {
		t.Fatalf("unrelated import changed: %+v", got.Imports[2])
	}
	got = SetRepoVersion(got, "github.com/acme/skills", "")
	if got.Imports[0].Version != "" || got.Imports[1].Version != "" {
		t.Fatalf("matching imports should be unpinned: %+v", got.Imports)
	}
}

func TestRemoveRepoDirRemovesConcreteDirAndImport(t *testing.T) {
	cfg := Config{Imports: []Import{
		{URL: "github.com/acme/skills", Dirs: []string{"one", "two"}},
		{URL: "github.com/acme/skills", Dirs: []string{"three"}},
		{URL: "github.com/other/skills", Dirs: []string{"one"}},
	}}
	got, err := RemoveRepoDir(cfg, "github.com/acme/skills", "two")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Imports[0].Dirs, ",") != "one" || strings.Join(got.Imports[1].Dirs, ",") != "three" {
		t.Fatalf("unexpected dirs after first remove: %+v", got.Imports)
	}
	got, err = RemoveRepoDir(got, "github.com/acme/skills", "three")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Imports) != 2 {
		t.Fatalf("empty import should be removed: %+v", got.Imports)
	}
	for _, imp := range got.Imports {
		if imp.URL == "github.com/acme/skills" && strings.Join(imp.Dirs, ",") == "three" {
			t.Fatalf("removed dir still configured: %+v", got.Imports)
		}
	}
}

func TestRemoveRepoDirBlocksWildcardImport(t *testing.T) {
	cases := []struct {
		cfg Config
		dir string
	}{
		{Config{Imports: []Import{{URL: "github.com/acme/skills"}}}, "one"},
		{Config{Imports: []Import{{URL: "github.com/acme/skills", Dirs: []string{"skills/*"}}}}, "skills/one"},
	}
	for _, c := range cases {
		_, err := RemoveRepoDir(c.cfg, "github.com/acme/skills", c.dir)
		if !errors.Is(err, ErrWildcardRemove) {
			t.Fatalf("want ErrWildcardRemove, got %v", err)
		}
	}
}

func TestReadImportsMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".skink.yaml", "imports:\n  - dirs:\n      - foo\n")
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Errorf("want url required, got %v", err)
	}
}

func TestReadImportsBadURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".skink.yaml", "imports:\n  - url: nonsense\n")
	_, err := ReadImports(dir)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestReadImportsBadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".skink.yaml", `
imports:
  - url: github.com/a/b
    dirs:
      - ../escape
`)
	_, err := ReadImports(dir)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestParseDir(t *testing.T) {
	cases := []struct {
		in       string
		wantPfx  string
		wildcard bool
		err      bool
	}{
		{"", "", true, false},
		{"*", "", true, false},
		{"/", "", true, false},
		{"foo", "foo", false, false},
		{"foo/", "foo", false, false},
		{"foo/bar", "foo/bar", false, false},
		{"foo/bar/", "foo/bar", false, false},
		{"foo/*", "foo", true, false},
		{"foo/bar/*", "foo/bar", true, false},
		{"/foo", "foo", false, false},
		{"../foo", "", false, true},
		{"foo/../bar", "", false, true},
		{"foo*bar", "", false, true},
		{"*/bar", "", false, true},
	}
	for _, c := range cases {
		got, err := ParseDir(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseDir(%q) want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDir(%q) err: %v", c.in, err)
			continue
		}
		if got.Prefix != c.wantPfx || got.Wildcard != c.wildcard {
			t.Errorf("ParseDir(%q) = %+v want {%q %v}", c.in, got, c.wantPfx, c.wildcard)
		}
	}
}

func TestParseGitURL(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		host string
		path string
	}{
		{"github.com/anthropics/skills", true, "github.com", "anthropics/skills"},
		{"github.com/anthropics/skills/", true, "github.com", "anthropics/skills"},
		{"https://github.com/anthropics/skills.git", true, "github.com", "anthropics/skills"},
		{"http://example.com/a/b", true, "example.com", "a/b"},
		{"git@github.com:anthropics/skills.git", true, "github.com", "anthropics/skills"},
		{"git@example.com:my-org/my-repo", true, "example.com", "my-org/my-repo"},
		{"ssh://git@example.com/my-org/my-repo.git", true, "example.com", "my-org/my-repo"},
		{"ssh://git@example.com:22/my-org/my-repo", true, "example.com", "my-org/my-repo"},
		{"git://github.com/a/b", true, "github.com", "a/b"},
		{"user@host.tld:just-repo", true, "host.tld", "just-repo"},
		{"", false, "", ""},
		{"no-scheme-no-slash", false, "", ""},
		{"host-only/", false, "", ""},
		{"ftp://host/path", false, "", ""},
		{"github.com/a/../b", false, "", ""},
	}
	for _, c := range cases {
		got, err := ParseGitURL(c.in)
		if c.ok != (err == nil) {
			t.Errorf("ParseGitURL(%q) ok=%v err=%v", c.in, err == nil, err)
			continue
		}
		if !c.ok {
			continue
		}
		if got.Host != c.host || got.Path != c.path {
			t.Errorf("ParseGitURL(%q) = %+v want {host=%q path=%q}", c.in, got, c.host, c.path)
		}
		if got.Original != c.in {
			t.Errorf("ParseGitURL(%q) Original = %q", c.in, got.Original)
		}
	}
}

func TestGitURLCloneURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/anthropics/skills", "https://github.com/anthropics/skills.git"},
		{"example.com/foo/bar", "https://example.com/foo/bar.git"},
		{"https://github.com/x/y.git", "https://github.com/x/y.git"},
		{"git@github.com:x/y.git", "git@github.com:x/y.git"},
		{"ssh://git@example.com/x/y", "ssh://git@example.com/x/y"},
	}
	for _, c := range cases {
		g, err := ParseGitURL(c.in)
		if err != nil {
			t.Fatalf("ParseGitURL(%q): %v", c.in, err)
		}
		if got := g.CloneURL(); got != c.want {
			t.Errorf("CloneURL(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestGitURLSegments(t *testing.T) {
	g, _ := ParseGitURL("git@example.com:my-org/my-repo.git")
	segs := g.CloneDirSegments()
	want := []string{"example.com", "my-org", "my-repo"}
	if strings.Join(segs, "/") != strings.Join(want, "/") {
		t.Errorf("segments = %v want %v", segs, want)
	}
	if got := g.DisplayPath(); got != "example.com/my-org/my-repo" {
		t.Errorf("DisplayPath = %q", got)
	}
}
