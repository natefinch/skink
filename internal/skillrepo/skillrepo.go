// Package skillrepo manages the local checkout of the user's skills git
// repository and enumerates the skills it contains.
//
// All git operations go through the GitRunner interface so tests can use a
// fake runner and avoid the network.
package skillrepo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// GitRunner executes a git subcommand. args are the arguments after "git".
// dir is the working directory to run in (may be "" for Clone's parent).
type GitRunner interface {
	Run(ctx context.Context, dir string, args ...string) error
}

// ExecGit is the production GitRunner, shelling out to the git binary.
// When Verbose is false (the default), stdout/stderr are suppressed and
// only surfaced if the command fails.
type ExecGit struct {
	Verbose bool
}

func (g ExecGit) Run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	if g.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = &buf
	}
	if err := cmd.Run(); err != nil {
		if g.Verbose || buf.Len() == 0 {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// Repo is a local skills-repo checkout rooted at Dir.
type Repo struct {
	Dir string
	Git GitRunner
}

// New returns a Repo using the provided checkout dir and GitRunner.
// If git is nil, ExecGit is used.
func New(dir string, git GitRunner) Repo {
	if git == nil {
		git = ExecGit{}
	}
	return Repo{Dir: dir, Git: git}
}

// Exists reports whether the checkout dir contains a .git directory.
func (r Repo) Exists() bool {
	info, err := os.Stat(filepath.Join(r.Dir, ".git"))
	return err == nil && info.IsDir()
}

// Clone clones url into r.Dir. The parent of r.Dir is created if missing.
// Returns an error if r.Dir already exists and is non-empty.
func (r Repo) Clone(ctx context.Context, url string) error {
	if url == "" {
		return fmt.Errorf("skillrepo: empty url")
	}
	if err := os.MkdirAll(filepath.Dir(r.Dir), 0o755); err != nil {
		return fmt.Errorf("skillrepo: mkdir parent: %w", err)
	}
	if entries, _ := os.ReadDir(r.Dir); len(entries) > 0 {
		return fmt.Errorf("skillrepo: %s already exists and is not empty", r.Dir)
	}
	return r.Git.Run(ctx, filepath.Dir(r.Dir), "clone", url, filepath.Base(r.Dir))
}

// Pull runs `git pull --ff-only` in the checkout.
func (r Repo) Pull(ctx context.Context) error {
	if !r.Exists() {
		return fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	return r.Git.Run(ctx, r.Dir, "pull", "--ff-only")
}

// Fetch runs `git fetch --tags` in the checkout.
func (r Repo) Fetch(ctx context.Context) error {
	if !r.Exists() {
		return fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	return r.Git.Run(ctx, r.Dir, "fetch", "--tags")
}

// Checkout runs `git checkout <ref>` in the checkout. The ref may be a
// branch name, tag, or commit SHA.
func (r Repo) Checkout(ctx context.Context, ref string) error {
	if !r.Exists() {
		return fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	if ref == "" {
		return fmt.Errorf("skillrepo: empty ref")
	}
	return r.Git.Run(ctx, r.Dir, "checkout", ref)
}

// Skill describes one installable skill in the repo.
type Skill struct {
	Name           string // directory basename (and frontmatter name)
	Path           string // absolute path to the skill directory on disk
	Source         string // "" for the primary repo, "host/path" otherwise
	InstallSubpath string // relative target path under a client's skills dir
}

// reservedTopLevel are directory names we never treat as skills even if the
// user creates them in the repo root.
var reservedTopLevel = map[string]struct{}{
	".git":    {},
	".github": {},
}

// List returns the skills available in the repo, sorted by name.
//
// A skill is any non-dot, non-reserved top-level directory.
func (r Repo) List() ([]Skill, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("skillrepo: read %s: %w", r.Dir, err)
	}
	var out []Skill
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		if _, ok := reservedTopLevel[name]; ok {
			continue
		}
		out = append(out, Skill{
			Name: name,
			Path: filepath.Join(r.Dir, name),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Find returns the skill with the given name, or false.
func (r Repo) Find(name string) (Skill, bool, error) {
	all, err := r.List()
	if err != nil {
		return Skill{}, false, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, true, nil
		}
	}
	return Skill{}, false, nil
}
