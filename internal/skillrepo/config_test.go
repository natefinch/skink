package skillrepo

import (
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
	got, err := ReadImports(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Imports != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestReadImportsAllFormats(t *testing.T) {
	cases := []struct {
		fname, body string
	}{
		{"skink.yaml", `
imports:
  - url: git@github.com:acme/team-skills.git
    dirs:
      - skills/foo
      - skills/bar/*
    version: v1
  - url: https://github.com/charm/skills.git
`},
		{"skink.json", `{"imports":[
  {"url":"git@github.com:acme/team-skills.git","dirs":["skills/foo","skills/bar/*"],"version":"v1"},
  {"url":"https://github.com/charm/skills.git"}
]}`},
		{"skink.toml", `
[[imports]]
url = "git@github.com:acme/team-skills.git"
dirs = ["skills/foo", "skills/bar/*"]
version = "v1"

[[imports]]
url = "https://github.com/charm/skills.git"
`},
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
			if got.Imports[0].URL != "git@github.com:acme/team-skills.git" || strings.Join(got.Imports[0].Dirs, ",") != "skills/foo,skills/bar/*" || got.Imports[0].Version != "v1" {
				t.Errorf("got.Imports[0] = %+v", got.Imports[0])
			}
			if got.Imports[1].URL != "https://github.com/charm/skills.git" {
				t.Errorf("got.Imports[1] = %+v", got.Imports[1])
			}
		})
	}
}

func TestReadImportsMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skink.yaml", "imports:\n  - dirs:\n      - foo\n")
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Errorf("want url required, got %v", err)
	}
}

func TestReadImportsBadURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skink.yaml", "imports:\n  - url: nonsense\n")
	_, err := ReadImports(dir)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestReadImportsBadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skink.yaml", `
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
