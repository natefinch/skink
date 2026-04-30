// Package syncer copies cached skill directories into a project and reports
// whether each destination was added, already current, overwritten, or in
// conflict.
package syncer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Item is one skill directory to sync.
type Item struct {
	Name   string
	Source string
}

// Conflict describes a destination that could not be synced safely.
type Conflict struct {
	Path   string
	Reason string
}

// Result summarizes a sync run.
type Result struct {
	Added       []string
	Existing    []string
	Overwritten []string
	Conflicts   []Conflict
}

// Status is the read-only state of one destination skill directory.
type Status string

const (
	StatusMissing   Status = "missing"
	StatusDifferent Status = "different"
	StatusUpToDate  Status = "up to date"
)

// StatusItem describes the read-only state of one skill directory.
type StatusItem struct {
	Name   string
	Source string
	Path   string
	Status Status
}

// Sync copies items into targetRoot. Existing destination directories must
// match their source exactly unless overwrite is true.
func Sync(items []Item, targetRoot string, overwrite bool) (Result, error) {
	if targetRoot == "" {
		return Result{}, fmt.Errorf("syncer: empty target root")
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("syncer: mkdir target root: %w", err)
	}

	var res Result
	duplicates := duplicateNames(items)
	for name, sources := range duplicates {
		sort.Strings(sources)
		res.Conflicts = append(res.Conflicts, Conflict{
			Path:   filepath.Join(targetRoot, name),
			Reason: fmt.Sprintf("multiple configured skills use directory name %q: %v", name, sources),
		})
	}

	seen := map[string]struct{}{}
	for _, item := range items {
		if item.Name == "" {
			return res, fmt.Errorf("syncer: empty skill name")
		}
		if item.Source == "" {
			return res, fmt.Errorf("syncer: empty source for %s", item.Name)
		}
		if _, dup := duplicates[item.Name]; dup {
			continue
		}
		if _, ok := seen[item.Name]; ok {
			continue
		}
		seen[item.Name] = struct{}{}

		dest := filepath.Join(targetRoot, item.Name)
		status, err := syncOne(item.Source, dest, overwrite)
		if err != nil {
			return res, err
		}
		switch status {
		case "added":
			res.Added = append(res.Added, dest)
		case "existing":
			res.Existing = append(res.Existing, dest)
		case "overwritten":
			res.Overwritten = append(res.Overwritten, dest)
		case "conflict":
			res.Conflicts = append(res.Conflicts, Conflict{Path: dest, Reason: "contents differ from cache"})
		}
	}
	sort.Strings(res.Added)
	sort.Strings(res.Existing)
	sort.Strings(res.Overwritten)
	sort.Slice(res.Conflicts, func(i, j int) bool { return res.Conflicts[i].Path < res.Conflicts[j].Path })
	return res, nil
}

// Check reports whether each configured skill directory exists and matches
// its source without changing the destination tree.
func Check(items []Item, targetRoot string) ([]StatusItem, error) {
	if targetRoot == "" {
		return nil, fmt.Errorf("syncer: empty target root")
	}

	var out []StatusItem
	duplicates := duplicateNames(items)
	for name, sources := range duplicates {
		sort.Strings(sources)
		out = append(out, StatusItem{
			Name:   name,
			Source: fmt.Sprintf("%v", sources),
			Path:   filepath.Join(targetRoot, name),
			Status: StatusDifferent,
		})
	}

	seen := map[string]struct{}{}
	for _, item := range items {
		if item.Name == "" {
			return out, fmt.Errorf("syncer: empty skill name")
		}
		if item.Source == "" {
			return out, fmt.Errorf("syncer: empty source for %s", item.Name)
		}
		if _, dup := duplicates[item.Name]; dup {
			continue
		}
		if _, ok := seen[item.Name]; ok {
			continue
		}
		seen[item.Name] = struct{}{}

		dest := filepath.Join(targetRoot, item.Name)
		status, err := checkOne(item.Source, dest)
		if err != nil {
			return out, err
		}
		out = append(out, StatusItem{
			Name:   item.Name,
			Source: item.Source,
			Path:   dest,
			Status: status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func duplicateNames(items []Item) map[string][]string {
	byName := map[string]map[string]struct{}{}
	for _, item := range items {
		if byName[item.Name] == nil {
			byName[item.Name] = map[string]struct{}{}
		}
		byName[item.Name][item.Source] = struct{}{}
	}
	out := map[string][]string{}
	for name, sources := range byName {
		if len(sources) <= 1 {
			continue
		}
		for source := range sources {
			out[name] = append(out[name], source)
		}
	}
	return out
}

func syncOne(source, dest string, overwrite bool) (string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("syncer: source %s: %w", source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("syncer: source %s is not a directory", source)
	}

	destInfo, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			if err := copyDir(source, dest); err != nil {
				return "", err
			}
			return "added", nil
		}
		return "", fmt.Errorf("syncer: lstat %s: %w", dest, err)
	}
	if !destInfo.IsDir() {
		return "conflict", nil
	}

	same, err := equalDir(source, dest)
	if err != nil {
		return "", err
	}
	if same {
		return "existing", nil
	}
	if !overwrite {
		return "conflict", nil
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("syncer: remove %s: %w", dest, err)
	}
	if err := copyDir(source, dest); err != nil {
		return "", err
	}
	return "overwritten", nil
}

func checkOne(source, dest string) (Status, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("syncer: source %s: %w", source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("syncer: source %s is not a directory", source)
	}

	destInfo, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return StatusMissing, nil
		}
		return "", fmt.Errorf("syncer: lstat %s: %w", dest, err)
	}
	if !destInfo.IsDir() {
		return StatusDifferent, nil
	}

	same, err := equalDir(source, dest)
	if err != nil {
		return "", err
	}
	if same {
		return StatusUpToDate, nil
	}
	return StatusDifferent, nil
}

type node struct {
	kind string
	data []byte
}

func equalDir(a, b string) (bool, error) {
	left, err := collect(a)
	if err != nil {
		return false, err
	}
	right, err := collect(b)
	if err != nil {
		return false, err
	}
	if len(left) != len(right) {
		return false, nil
	}
	for path, ln := range left {
		rn, ok := right[path]
		if !ok || ln.kind != rn.kind || !bytes.Equal(ln.data, rn.data) {
			return false, nil
		}
	}
	return true, nil
}

func collect(root string) (map[string]node, error) {
	out := map[string]node{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			out[rel] = node{kind: "dir"}
		case mode.Type()&os.ModeSymlink != 0:
			dest, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = node{kind: "symlink", data: []byte(dest)}
		case mode.IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[rel] = node{kind: "file", data: data}
		default:
			return fmt.Errorf("syncer: unsupported file type %s", p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("syncer: walk %s: %w", root, err)
	}
	return out, nil
}

func copyDir(source, dest string) error {
	rootInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("syncer: stat %s: %w", source, err)
	}
	err = filepath.WalkDir(source, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		target := dest
		if rel != "." {
			target = filepath.Join(dest, rel)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			perm := mode.Perm()
			if rel == "." {
				perm = rootInfo.Mode().Perm()
			}
			return os.MkdirAll(target, perm)
		case mode.Type()&os.ModeSymlink != 0:
			linkDest, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(linkDest, target)
		case mode.IsRegular():
			return copyFile(p, target, mode.Perm())
		default:
			return fmt.Errorf("syncer: unsupported file type %s", p)
		}
	})
	if err != nil {
		return fmt.Errorf("syncer: copy %s to %s: %w", source, dest, err)
	}
	return nil
}

func copyFile(source, dest string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}
