package syncer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAddsMissingDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cache", "skill")
	target := filepath.Join(root, "repo", "skills")
	write(t, filepath.Join(src, "SKILL.md"), "name: skill\n")

	res, err := Sync([]Item{{Name: "skill", Source: src}}, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || len(res.Existing) != 0 || len(res.Conflicts) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if got, err := os.ReadFile(filepath.Join(target, "skill", "SKILL.md")); err != nil || string(got) != "name: skill\n" {
		t.Fatalf("copied file = %q err=%v", got, err)
	}
}

func TestSyncReportsExistingWhenEqual(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cache", "skill")
	target := filepath.Join(root, "repo", "skills")
	write(t, filepath.Join(src, "SKILL.md"), "same\n")
	write(t, filepath.Join(target, "skill", "SKILL.md"), "same\n")

	res, err := Sync([]Item{{Name: "skill", Source: src}}, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Existing) != 1 || len(res.Added) != 0 || len(res.Conflicts) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSyncReportsConflictWhenDifferent(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cache", "skill")
	target := filepath.Join(root, "repo", "skills")
	write(t, filepath.Join(src, "SKILL.md"), "source\n")
	write(t, filepath.Join(target, "skill", "SKILL.md"), "dest\n")

	res, err := Sync([]Item{{Name: "skill", Source: src}}, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("expected conflict: %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "skill", "SKILL.md")); string(got) != "dest\n" {
		t.Fatalf("conflict should not overwrite destination, got %q", got)
	}
}

func TestSyncOverwriteMakesDestinationExact(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cache", "skill")
	target := filepath.Join(root, "repo", "skills")
	write(t, filepath.Join(src, "SKILL.md"), "source\n")
	write(t, filepath.Join(target, "skill", "SKILL.md"), "dest\n")
	write(t, filepath.Join(target, "skill", "extra.txt"), "remove me\n")

	res, err := Sync([]Item{{Name: "skill", Source: src}}, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Overwritten) != 1 || len(res.Conflicts) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "skill", "SKILL.md")); string(got) != "source\n" {
		t.Fatalf("destination not overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target, "skill", "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra file should be removed, err=%v", err)
	}
}

func TestSyncDuplicateNamesConflict(t *testing.T) {
	root := t.TempDir()
	srcA := filepath.Join(root, "cache", "a")
	srcB := filepath.Join(root, "cache", "b")
	write(t, filepath.Join(srcA, "SKILL.md"), "a\n")
	write(t, filepath.Join(srcB, "SKILL.md"), "b\n")

	res, err := Sync([]Item{{Name: "skill", Source: srcA}, {Name: "skill", Source: srcB}}, filepath.Join(root, "skills"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0].Reason, "multiple configured skills") {
		t.Fatalf("expected duplicate conflict: %+v", res)
	}
}

func TestSyncCopiesSymlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cache", "skill")
	target := filepath.Join(root, "repo", "skills")
	write(t, filepath.Join(src, "target.txt"), "target\n")
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync([]Item{{Name: "skill", Source: src}}, target, false); err != nil {
		t.Fatal(err)
	}
	dest, err := os.Readlink(filepath.Join(target, "skill", "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if dest != "target.txt" {
		t.Fatalf("symlink target = %q", dest)
	}
}

func TestCheckReportsMissingDifferentAndUpToDate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "repo", "skills")
	srcMissing := filepath.Join(root, "cache", "b-missing")
	srcDifferent := filepath.Join(root, "cache", "c-different")
	srcCurrent := filepath.Join(root, "cache", "a-current")
	write(t, filepath.Join(srcMissing, "SKILL.md"), "missing\n")
	write(t, filepath.Join(srcDifferent, "SKILL.md"), "source\n")
	write(t, filepath.Join(srcCurrent, "SKILL.md"), "same\n")
	write(t, filepath.Join(target, "c-different", "SKILL.md"), "dest\n")
	write(t, filepath.Join(target, "a-current", "SKILL.md"), "same\n")

	got, err := Check([]Item{
		{Name: "b-missing", Source: srcMissing},
		{Name: "c-different", Source: srcDifferent},
		{Name: "a-current", Source: srcCurrent},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("statuses = %+v", got)
	}
	want := []struct {
		name   string
		status Status
	}{
		{"a-current", StatusUpToDate},
		{"b-missing", StatusMissing},
		{"c-different", StatusDifferent},
	}
	for i := range want {
		if got[i].Name != want[i].name || got[i].Status != want[i].status {
			t.Fatalf("status[%d] = %+v, want %s %s", i, got[i], want[i].name, want[i].status)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "b-missing")); !os.IsNotExist(err) {
		t.Fatalf("Check should not create missing destination, err=%v", err)
	}
}

func TestCheckReportsDuplicateNameAsDifferent(t *testing.T) {
	root := t.TempDir()
	srcA := filepath.Join(root, "cache", "a")
	srcB := filepath.Join(root, "cache", "b")
	write(t, filepath.Join(srcA, "SKILL.md"), "a\n")
	write(t, filepath.Join(srcB, "SKILL.md"), "b\n")

	got, err := Check([]Item{{Name: "skill", Source: srcA}, {Name: "skill", Source: srcB}}, filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "skill" || got[0].Status != StatusDifferent {
		t.Fatalf("duplicate status = %+v", got)
	}
}
