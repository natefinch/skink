package skillrepo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Tag describes one git tag and its creation date as reported by git.
type Tag struct {
	Name    string
	Created string
}

// Tags returns all tags in the repo with their git creator dates.
func (r Repo) Tags(ctx context.Context) ([]Tag, error) {
	if !r.Exists() {
		return nil, fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	out, err := r.output(ctx,
		"for-each-ref",
		"--sort=-creatordate",
		"--format=%(refname:short)%00%(creatordate:iso8601-strict)",
		"refs/tags",
	)
	if err != nil {
		return nil, err
	}
	return parseTagOutput(out), nil
}

// RemoteTags returns tags advertised by origin. Git does not include tag
// creation dates in ls-remote output, so Created is empty for these tags.
func (r Repo) RemoteTags(ctx context.Context) ([]Tag, error) {
	if !r.Exists() {
		return nil, fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	out, err := r.output(ctx, "ls-remote", "--tags", "--refs", "origin")
	if err != nil {
		return nil, err
	}
	return parseRemoteTagOutput(out), nil
}

func parseTagOutput(out string) []Tag {
	var tags []Tag
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		name, created, ok := strings.Cut(line, "\x00")
		if !ok {
			name = line
		}
		tags = append(tags, Tag{Name: strings.TrimSpace(name), Created: strings.TrimSpace(created)})
	}
	return tags
}

func parseRemoteTagOutput(out string) []Tag {
	var tags []Tag
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/tags/")
		if name == "" {
			continue
		}
		tags = append(tags, Tag{Name: name})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return tags
}

// MergeTags merges tag lists by name, preserving local creation dates when
// present and adding tags advertised by the remote but absent locally.
func MergeTags(lists ...[]Tag) []Tag {
	byName := map[string]Tag{}
	for _, tags := range lists {
		for _, tag := range tags {
			if tag.Name == "" {
				continue
			}
			existing, ok := byName[tag.Name]
			if !ok || existing.Created == "" {
				byName[tag.Name] = tag
			}
		}
	}
	out := make([]Tag, 0, len(byName))
	for _, tag := range byName {
		out = append(out, tag)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RemoteHeadChanged reports whether HEAD differs from origin's default HEAD.
func (r Repo) RemoteHeadChanged(ctx context.Context) (bool, error) {
	if !r.Exists() {
		return false, fmt.Errorf("skillrepo: %s is not a git checkout", r.Dir)
	}
	local, err := r.output(ctx, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	remote, err := r.output(ctx, "ls-remote", "origin", "HEAD")
	if err != nil {
		return false, err
	}
	remoteHash := strings.Fields(remote)
	if len(remoteHash) == 0 {
		return false, fmt.Errorf("skillrepo: origin HEAD not found")
	}
	return strings.TrimSpace(local) != remoteHash[0], nil
}

// SemverTags returns semver-like tags sorted newest first.
func SemverTags(tags []Tag) []Tag {
	var out []Tag
	for _, tag := range tags {
		if _, ok := parseSemver(tag.Name); ok {
			out = append(out, tag)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		vi, _ := parseSemver(out[i].Name)
		vj, _ := parseSemver(out[j].Name)
		return compareSemver(vi, vj) > 0
	})
	return out
}

// NewerSemverTags returns semver tags newer than current, newest first.
func NewerSemverTags(tags []Tag, current string) ([]Tag, bool) {
	cur, ok := parseSemver(current)
	if !ok {
		return nil, false
	}
	var out []Tag
	for _, tag := range SemverTags(tags) {
		v, _ := parseSemver(tag.Name)
		if compareSemver(v, cur) > 0 {
			out = append(out, tag)
		}
	}
	return out, true
}

type semver struct {
	major int
	minor int
	patch int
	pre   string
}

func parseSemver(tag string) (semver, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	main, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i, part := range parts {
		if part == "" {
			return semver{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, true
}

func compareSemver(a, b semver) int {
	switch {
	case a.major != b.major:
		return compareInt(a.major, b.major)
	case a.minor != b.minor:
		return compareInt(a.minor, b.minor)
	case a.patch != b.patch:
		return compareInt(a.patch, b.patch)
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	default:
		return strings.Compare(a.pre, b.pre)
	}
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
