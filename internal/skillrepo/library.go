package skillrepo

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
)

// Library is the full set of skill sources available to skillnk: the user's
// primary skills checkout plus any imports declared in its config file.
type Library struct {
	Primary Repo
	Imports []Source
}

// Source is one repo in a Library with the name it should be referred to by.
// Name is "" for the primary repo, and the import name otherwise.
type Source struct {
	Name string
	Repo Repo
}

// NewLibrary constructs a Library for the given skillnk home dir. The
// primary checkout lives at <home>/repo; imports live at <home>/<name>.
// Imports are read from the primary repo's skillnk config file. Imports of
// imports are not followed.
func NewLibrary(home string, git GitRunner) (Library, error) {
	primaryDir := filepath.Join(home, "repo")
	primary := New(primaryDir, git)
	lib := Library{Primary: primary}
	if !primary.Exists() {
		return lib, nil
	}
	imports, err := ReadImports(primaryDir)
	if err != nil {
		return lib, err
	}
	for _, imp := range imports {
		dir := filepath.Join(home, imp.Name)
		lib.Imports = append(lib.Imports, Source{
			Name: imp.Name,
			Repo: New(dir, git),
		})
	}
	return lib, nil
}

// EnsureImportsCloned clones any import repos that are not yet present on
// disk. Each import URL comes from the primary repo's config.
func (l Library) EnsureImportsCloned(ctx context.Context, home string) error {
	primaryDir := l.Primary.Dir
	imports, err := ReadImports(primaryDir)
	if err != nil {
		return err
	}
	for _, imp := range imports {
		dir := filepath.Join(home, imp.Name)
		r := New(dir, l.Primary.Git)
		if r.Exists() {
			continue
		}
		if err := r.Clone(ctx, imp.URL); err != nil {
			return fmt.Errorf("clone import %q: %w", imp.Name, err)
		}
	}
	return nil
}

// PullAll runs git pull on the primary repo and every import. Errors from
// individual imports are collected and returned after all pulls are
// attempted so one broken import doesn't block the rest.
func (l Library) PullAll(ctx context.Context) error {
	var errs []error
	if err := l.Primary.Pull(ctx); err != nil {
		errs = append(errs, fmt.Errorf("primary: %w", err))
	}
	for _, imp := range l.Imports {
		if !imp.Repo.Exists() {
			continue
		}
		if err := imp.Repo.Pull(ctx); err != nil {
			errs = append(errs, fmt.Errorf("import %q: %w", imp.Name, err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	// Combine errors
	var msg string
	for i, e := range errs {
		if i > 0 {
			msg += "; "
		}
		msg += e.Error()
	}
	return fmt.Errorf("pull errors: %s", msg)
}

// ListAll returns every skill available in the library, with Source set to
// the owning repo's name ("" for the primary repo). Skills are ordered by
// the repo order (primary first, then imports in config order) and by name
// within each repo. When the same skill name appears in more than one
// source, the first occurrence wins (primary beats imports, earlier imports
// beat later ones); duplicates are dropped from the result.
func (l Library) ListAll() ([]Skill, error) {
	seen := map[string]struct{}{}
	var out []Skill

	appendFrom := func(sourceName string, r Repo) error {
		if !r.Exists() {
			return nil
		}
		skills, err := r.List()
		if err != nil {
			return err
		}
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		for _, s := range skills {
			if _, dup := seen[s.Name]; dup {
				continue
			}
			seen[s.Name] = struct{}{}
			s.Source = sourceName
			out = append(out, s)
		}
		return nil
	}

	if err := appendFrom("", l.Primary); err != nil {
		return nil, err
	}
	for _, imp := range l.Imports {
		if err := appendFrom(imp.Name, imp.Repo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Find returns the first skill matching name across the library.
func (l Library) Find(name string) (Skill, bool, error) {
	skills, err := l.ListAll()
	if err != nil {
		return Skill{}, false, err
	}
	for _, s := range skills {
		if s.Name == name {
			return s, true, nil
		}
	}
	return Skill{}, false, nil
}
