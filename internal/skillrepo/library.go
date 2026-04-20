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
// Name is "" for the primary repo, and the import name otherwise. Version
// is the pinned git ref for imports, "" for the primary repo or unpinned
// imports.
type Source struct {
	Name    string
	Version string
	Repo    Repo
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
			Name:    imp.Name,
			Version: imp.Version,
			Repo:    New(dir, git),
		})
	}
	return lib, nil
}

// EnsureImportsCloned clones any import repos that are not yet present on
// disk. For imports with a pinned version, the ref is checked out after
// cloning.
func (l Library) EnsureImportsCloned(ctx context.Context, home string) error {
	imports, err := ReadImports(l.Primary.Dir)
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
		if imp.Version != "" {
			if err := r.Checkout(ctx, imp.Version); err != nil {
				return fmt.Errorf("checkout import %q @ %s: %w", imp.Name, imp.Version, err)
			}
		}
	}
	return nil
}

// PullAll updates the primary repo and every import. The primary repo is
// always pulled with `git pull --ff-only`. For imports with a pinned
// version, skillnk runs `git fetch --tags` followed by `git checkout
// <version>`; unpinned imports are pulled with `git pull --ff-only`.
//
// Errors from individual imports are collected and returned after all
// updates are attempted so one broken import doesn't block the rest.
func (l Library) PullAll(ctx context.Context) error {
	var errs []error
	if err := l.Primary.Pull(ctx); err != nil {
		errs = append(errs, fmt.Errorf("primary: %w", err))
	}
	for _, imp := range l.Imports {
		if !imp.Repo.Exists() {
			continue
		}
		var err error
		if imp.Version != "" {
			if err = imp.Repo.Fetch(ctx); err == nil {
				err = imp.Repo.Checkout(ctx, imp.Version)
			}
		} else {
			err = imp.Repo.Pull(ctx)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("import %q: %w", imp.Name, err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
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
