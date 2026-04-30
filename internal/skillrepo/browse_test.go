package skillrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillsReadsSkillMetadata(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "skills", "writer"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "skills", "writer", "SKILL.md"), []byte(`---
name: writer
description: Writes things.
---

# Ignored
`), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, ".git", "ignored"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".git", "ignored", "SKILL.md"), []byte("---\nname: ignored\n---\n"), 0o644))

	got, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "writer" || got[0].Description != "Writes things." || got[0].RelDir != "skills/writer" {
		t.Fatalf("unexpected skill: %+v", got[0])
	}
}

func TestDiscoverSkillsFallsBackToHeadingAndBasename(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "no-frontmatter"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "no-frontmatter", "SKILL.md"), []byte("# Heading Name\n\nFirst paragraph.\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "basename"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "basename", "SKILL.md"), []byte("Description only.\n"), 0o644))

	got, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]DiscoveredSkill{}
	for _, s := range got {
		byPath[s.RelDir] = s
	}
	if byPath["no-frontmatter"].Name != "Heading Name" || byPath["no-frontmatter"].Description != "First paragraph." {
		t.Fatalf("heading fallback failed: %+v", byPath["no-frontmatter"])
	}
	if byPath["basename"].Name != "basename" || byPath["basename"].Description != "Description only." {
		t.Fatalf("basename fallback failed: %+v", byPath["basename"])
	}
}
