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
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestReadImportsYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", `
imports:
  - name: team-skills
    url: git@github.com:acme/team-skills.git
  - url: https://github.com/charm/skills.git
`)
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "team-skills" || got[0].URL != "git@github.com:acme/team-skills.git" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Name != "charm/skills" || got[1].URL != "https://github.com/charm/skills.git" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestReadImportsJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.json", `{"imports":[{"url":"https://github.com/a/b"}]}`)
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a/b" {
		t.Errorf("got %+v", got)
	}
}

func TestReadImportsTOML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.toml", `
[[imports]]
url = "git@github.com:me/r.git"
name = "mine"
`)
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "mine" {
		t.Errorf("got %+v", got)
	}
}

func TestReadImportsYML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yml", "imports:\n - url: https://github.com/x/y\n")
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "x/y" {
		t.Errorf("got %+v", got)
	}
}

func TestReadImportsPriority(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", "imports:\n - url: https://github.com/first/first\n")
	writeFile(t, dir, "skillnk.json", `{"imports":[{"url":"https://github.com/second/second"}]}`)
	got, err := ReadImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "first/first" {
		t.Errorf("yaml should win: %+v", got)
	}
}

func TestReadImportsMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", "imports:\n - name: foo\n")
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Errorf("want url required, got %v", err)
	}
}

func TestReadImportsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", `
imports:
  - name: dup
    url: https://github.com/a/b
  - name: dup
    url: https://github.com/c/d
`)
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("want duplicate error, got %v", err)
	}
}

func TestReadImportsReservedName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", `
imports:
  - name: repo
    url: https://github.com/a/b
`)
	_, err := ReadImports(dir)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("want reserved error, got %v", err)
	}
}

func TestReadImportsBadName(t *testing.T) {
	cases := []string{".hidden", "a/../b", "/abs"}
	for _, n := range cases {
		dir := t.TempDir()
		writeFile(t, dir, "skillnk.yaml", "imports:\n - name: "+n+"\n   url: https://github.com/a/b\n")
		if _, err := ReadImports(dir); err == nil {
			t.Errorf("name %q should be rejected", n)
		}
	}
}

func TestDefaultImportName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo":          "owner/repo",
		"https://github.com/owner/repo.git":      "owner/repo",
		"http://github.com/owner/repo":           "owner/repo",
		"git@github.com:owner/repo.git":          "owner/repo",
		"ssh://git@github.com/owner/repo.git":    "owner/repo",
		"github.com/owner/repo":                  "owner/repo",
		"github.com:owner/repo":                  "owner/repo",
		"https://gitlab.example.com/team/repo.git": "repo",
		"https://example.com/x.git":              "x",
		"":                                       "import",
		"/":                                      "import",
	}
	for in, want := range cases {
		if got := DefaultImportName(in); got != want {
			t.Errorf("DefaultImportName(%q) = %q want %q", in, got, want)
		}
	}
}

func TestReadImportsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skillnk.yaml", "::: not yaml :::\n\t-[")
	_, err := ReadImports(dir)
	if err == nil {
		t.Error("want parse error")
	}
}
