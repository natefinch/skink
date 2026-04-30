package skillrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Library is the full set of external skill sources declared by a project's
// skink config file.
type Library struct {
	Config  Config
	Sources []Source
}

// Source is one external repo managed by skink — a unique clone on disk.
// Multiple imports can resolve to the same Source (for example, two
// imports with different `dirs` selectors but the same `url`); they are
// merged into a single Source with one Imports entry per original import.
type Source struct {
	URL     GitURL
	Version string
	Imports []Import // one or more imports that resolve to this clone
	Repo    Repo
}

// NewLibrary constructs a Library from the skink config in projectRoot.
// External source repos are cloned under cacheHome. Imports of imports are
// not followed.
func NewLibrary(projectRoot, cacheHome string, git GitRunner) (Library, error) {
	var lib Library
	cfg, err := ReadImports(projectRoot)
	if err != nil {
		return lib, err
	}
	lib.Config = cfg

	byDir := map[string]int{} // clone dir -> index into lib.Sources
	for _, imp := range cfg.Imports {
		u, err := ParseGitURL(imp.URL)
		if err != nil {
			return lib, err
		}
		cloneDir := filepath.Join(append([]string{cacheHome}, u.CloneDirSegments()...)...)
		if idx, ok := byDir[cloneDir]; ok {
			existing := &lib.Sources[idx]
			if existing.Version != imp.Version {
				return lib, fmt.Errorf(
					"skillrepo: repo %s pinned to conflicting versions: %q vs %q",
					u.DisplayPath(), existing.Version, imp.Version,
				)
			}
			existing.Imports = append(existing.Imports, imp)
			continue
		}
		byDir[cloneDir] = len(lib.Sources)
		lib.Sources = append(lib.Sources, Source{
			URL:     u,
			Version: imp.Version,
			Imports: []Import{imp},
			Repo:    New(cloneDir, git),
		})
	}
	return lib, nil
}

// EnsureCloned clones any source repo that is not yet present on disk.
// Pinned sources are checked out to their version after cloning.
func (l Library) EnsureCloned(ctx context.Context) error {
	for _, s := range l.Sources {
		if s.Repo.Exists() {
			continue
		}
		if err := s.Repo.Clone(ctx, s.URL.CloneURL()); err != nil {
			return fmt.Errorf("clone %s: %w", s.URL.DisplayPath(), err)
		}
		if s.Version != "" {
			if err := s.Repo.Checkout(ctx, s.Version); err != nil {
				return fmt.Errorf("checkout %s @ %s: %w", s.URL.DisplayPath(), s.Version, err)
			}
		}
	}
	return nil
}

// PullAll updates every Source. Pinned sources are fetched + checked out to
// their version; unpinned sources get `git pull --ff-only`. Errors from
// individual sources are aggregated.
func (l Library) PullAll(ctx context.Context) error {
	var errs []error
	for _, s := range l.Sources {
		if !s.Repo.Exists() {
			continue
		}
		var err error
		if s.Version != "" {
			if err = s.Repo.Fetch(ctx); err == nil {
				err = s.Repo.Checkout(ctx, s.Version)
			}
		} else {
			err = s.Repo.Pull(ctx)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.URL.DisplayPath(), err))
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

// ListAll returns every skill available in the library.
//
// For each Source in declaration order, each of its Imports is expanded
// against the on-disk clone: `dirs: [foo]` produces one skill at
// `<clone>/foo`, `dirs: [foo/*]` or the default produces one skill for every
// immediate subdirectory. Skills carry an InstallSubpath of
// `<host>/<path>/<skill-subpath>` so different sources don't collide.
//
// If a Source's repo isn't cloned yet (or a referenced subdirectory
// doesn't exist), its skills are silently skipped — run EnsureCloned first
// to get a complete listing.
func (l Library) ListAll() ([]Skill, error) {
	var out []Skill

	for _, src := range l.Sources {
		if !src.Repo.Exists() {
			continue
		}
		basePath := filepath.Join(src.URL.CloneDirSegments()...)
		for _, imp := range src.Imports {
			for _, dir := range importDirs(imp) {
				sel, err := ParseDir(dir)
				if err != nil {
					return nil, err
				}
				skills, err := expandSelector(src.Repo.Dir, sel)
				if err != nil {
					return nil, err
				}
				for _, rs := range skills {
					sub := filepath.FromSlash(rs.subpath)
					out = append(out, Skill{
						Name:           rs.name,
						Path:           rs.path,
						Source:         src.URL.DisplayPath(),
						SourceURL:      src.URL.Original,
						SourceDir:      rs.subpath,
						Version:        src.Version,
						InstallSubpath: filepath.Join(basePath, sub),
					})
				}
			}
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

type resolvedSkill struct {
	name    string
	path    string // absolute path to skill dir on disk
	subpath string // path relative to the clone root
}

// expandSelector walks the clone to produce zero or more resolvedSkills for
// one dir selector. Nonexistent paths yield an empty slice, not an error,
// so a pinned skill temporarily missing from a branch doesn't block other
// skills from listing.
func expandSelector(repoDir string, sel DirSelector) ([]resolvedSkill, error) {
	var out []resolvedSkill
	if sel.Wildcard {
		base := repoDir
		if sel.Prefix != "" {
			base = filepath.Join(repoDir, sel.Prefix)
		}
		info, err := os.Stat(base)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, nil
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() {
				continue
			}
			if sel.Prefix == "" {
				// repo root: skip dotfiles and reserved names.
				if _, skip := reservedTopLevel[name]; skip {
					continue
				}
				if name[0] == '.' {
					continue
				}
			}
			sub := name
			if sel.Prefix != "" {
				sub = sel.Prefix + "/" + name
			}
			out = append(out, resolvedSkill{
				name:    name,
				path:    filepath.Join(base, name),
				subpath: sub,
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].subpath < out[j].subpath })
		return out, nil
	}
	// Single directory.
	full := filepath.Join(repoDir, sel.Prefix)
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	out = append(out, resolvedSkill{
		name:    filepath.Base(sel.Prefix),
		path:    full,
		subpath: sel.Prefix,
	})
	return out, nil
}
