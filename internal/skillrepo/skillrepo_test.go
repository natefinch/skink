package skillrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit records calls and can simulate a clone by creating .git.
type fakeGit struct {
	calls       [][]string
	outputs     map[string]string
	cloneCreate bool
	err         error
}

func (f *fakeGit) Run(_ context.Context, dir string, args ...string) error {
	f.calls = append(f.calls, append([]string{dir}, args...))
	if f.err != nil {
		return f.err
	}
	if f.cloneCreate && len(args) >= 3 && args[0] == "clone" {
		target := filepath.Join(dir, args[2])
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeGit) RunOutput(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{dir}, args...))
	if f.err != nil {
		return "", f.err
	}
	if f.outputs == nil {
		return "", nil
	}
	return f.outputs[strings.Join(args, " ")], nil
}

func TestExistsFalse(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "none"), &fakeGit{})
	if r.Exists() {
		t.Error("should not exist")
	}
}

func TestExistsTrue(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(dir, &fakeGit{})
	if !r.Exists() {
		t.Error("should exist")
	}
}

func TestCloneEmptyURL(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "repo"), &fakeGit{})
	if err := r.Clone(context.Background(), ""); err == nil {
		t.Error("empty url should error")
	}
}

func TestCloneCallsGit(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "repo")
	g := &fakeGit{cloneCreate: true}
	r := New(dir, g)
	if err := r.Clone(context.Background(), "git@x:y.git"); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 1 {
		t.Fatalf("calls: %v", g.calls)
	}
	call := g.calls[0]
	if call[0] != parent {
		t.Errorf("dir = %q want %q", call[0], parent)
	}
	if call[1] != "clone" || call[2] != "git@x:y.git" || call[3] != "repo" {
		t.Errorf("args = %v", call[1:])
	}
	if !r.Exists() {
		t.Error("expected checkout to exist after clone")
	}
}

func TestCloneRefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(dir, &fakeGit{})
	err := r.Clone(context.Background(), "git@x:y.git")
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("want non-empty error, got %v", err)
	}
}

func TestPullRequiresCheckout(t *testing.T) {
	r := New(t.TempDir(), &fakeGit{})
	if err := r.Pull(context.Background()); err == nil {
		t.Error("pull without .git should fail")
	}
}

func TestPullCallsGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{}
	r := New(dir, g)
	if err := r.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 1 || g.calls[0][1] != "pull" {
		t.Errorf("unexpected calls: %v", g.calls)
	}
}

func TestPullPropagatesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(dir, &fakeGit{err: errors.New("nope")})
	if err := r.Pull(context.Background()); err == nil {
		t.Error("want error from git")
	}
}

func setupRepo(t *testing.T, names ...string) Repo {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return New(dir, &fakeGit{})
}

func TestListFiltersAndSorts(t *testing.T) {
	r := setupRepo(t, "zeta", "alpha", ".git", ".github", "beta")
	// also add a file at top level
	if err := os.WriteFile(filepath.Join(r.Dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, s := range got {
		if s.Name != want[i] {
			t.Errorf("got[%d] = %q want %q", i, s.Name, want[i])
		}
		if s.Path != filepath.Join(r.Dir, s.Name) {
			t.Errorf("path = %q", s.Path)
		}
	}
}

func TestListMissingDir(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "nope"), &fakeGit{})
	if _, err := r.List(); err == nil {
		t.Error("missing dir should error")
	}
}

func TestFind(t *testing.T) {
	r := setupRepo(t, "alpha", "beta")
	s, ok, err := r.Find("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || s.Name != "alpha" {
		t.Errorf("find alpha: %+v ok=%v", s, ok)
	}
	_, ok, err = r.Find("missing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("missing should be false")
	}
}

func TestTagsParsesGitOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{outputs: map[string]string{
		"for-each-ref --sort=-creatordate --format=%(refname:short)%00%(creatordate:iso8601-strict) refs/tags": "v1.2.0\x002026-01-02T03:04:05Z\nv1.1.0\x002026-01-01T03:04:05Z\n",
	}}
	got, err := New(dir, g).Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "v1.2.0" || got[0].Created != "2026-01-02T03:04:05Z" {
		t.Fatalf("tags = %+v", got)
	}
}

func TestRemoteTagsParsesLsRemoteOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{outputs: map[string]string{
		"ls-remote --tags --refs origin": "abc\trefs/tags/v1.0.0\ndef\trefs/tags/v1.2.0\n",
	}}
	got, err := New(dir, g).RemoteTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "v1.0.0" || got[1].Name != "v1.2.0" {
		t.Fatalf("remote tags = %+v", got)
	}
}

func TestMergeTagsPreservesLocalDates(t *testing.T) {
	got := MergeTags(
		[]Tag{{Name: "v1.0.0", Created: "date"}, {Name: "v1.1.0"}},
		[]Tag{{Name: "v1.0.0"}, {Name: "v1.2.0"}},
	)
	want := map[string]string{
		"v1.0.0": "date",
		"v1.1.0": "",
		"v1.2.0": "",
	}
	if len(got) != len(want) {
		t.Fatalf("merged tags = %+v", got)
	}
	for _, tag := range got {
		if want[tag.Name] != tag.Created {
			t.Fatalf("tag %s created = %q want %q; all=%+v", tag.Name, tag.Created, want[tag.Name], got)
		}
	}
}

func TestSemverTagsSortNewestFirst(t *testing.T) {
	got := SemverTags([]Tag{{Name: "v1.2.9"}, {Name: "v1.2.10"}, {Name: "notes"}, {Name: "1.3.0"}})
	want := []string{"1.3.0", "v1.2.10", "v1.2.9"}
	if len(got) != len(want) {
		t.Fatalf("semver tags = %+v", got)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("semver tag %d = %q want %q", i, got[i].Name, want[i])
		}
	}
}

func TestNewerSemverTags(t *testing.T) {
	got, ok := NewerSemverTags([]Tag{{Name: "v1.0.0"}, {Name: "v1.2.0"}, {Name: "v2.0.0"}}, "v1.1.0")
	if !ok {
		t.Fatal("current tag should parse as semver")
	}
	want := []string{"v2.0.0", "v1.2.0"}
	if len(got) != len(want) {
		t.Fatalf("newer tags = %+v", got)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("newer tag %d = %q want %q", i, got[i].Name, want[i])
		}
	}
	if _, ok := NewerSemverTags([]Tag{{Name: "v1.0.0"}}, "release-a"); ok {
		t.Fatal("non-semver current tag should not be semver comparable")
	}
}

func TestRemoteHeadChanged(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{outputs: map[string]string{
		"rev-parse HEAD":        "abc\n",
		"ls-remote origin HEAD": "def\tHEAD\n",
	}}
	changed, err := New(dir, g).RemoteHeadChanged(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("remote head should be reported changed")
	}
}
