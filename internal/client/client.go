// Package client describes the AI clients skink can install skills into
// and how to detect which one a project uses.
package client

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Client describes one supported AI client.
type Client struct {
	// Name is the canonical short name (claude, copilot, cursor, codex).
	Name string
	// MarkerDir is a directory whose presence in the project root indicates
	// this client is in use.
	MarkerDir string
	// SkillsSubdir is the path, relative to the project root, where skills
	// should be installed. Each installed skill becomes a directory inside it.
	SkillsSubdir string
}

// TargetFor returns the absolute directory where a skill should be
// symlinked, given a project root and a skill-specific install subpath
// (which may itself contain directory segments for URL-namespaced imports).
func (c Client) TargetFor(projectRoot, installSubpath string) string {
	return filepath.Join(projectRoot, c.SkillsSubdir, installSubpath)
}

// Registry is the built-in list of supported clients. Order is the preferred
// display order.
var Registry = []Client{
	{Name: "claude", MarkerDir: ".claude", SkillsSubdir: ".claude/skills"},
	{Name: "copilot", MarkerDir: ".github", SkillsSubdir: ".github/skills"},
	{Name: "cursor", MarkerDir: ".cursor", SkillsSubdir: ".cursor/skills"},
	{Name: "codex", MarkerDir: ".codex", SkillsSubdir: ".codex/skills"},
}

// ByName looks up a client by its canonical name.
func ByName(name string) (Client, bool) {
	for _, c := range Registry {
		if c.Name == name {
			return c, true
		}
	}
	return Client{}, false
}

// Names returns the canonical names of all registered clients, in registry
// order.
func Names() []string {
	out := make([]string, len(Registry))
	for i, c := range Registry {
		out[i] = c.Name
	}
	return out
}

// Detect returns every client whose marker directory exists under
// projectRoot. The result is sorted by registry order. A project with no
// matches returns an empty slice (no error).
func Detect(projectRoot string) ([]Client, error) {
	var found []Client
	for _, c := range Registry {
		info, err := os.Stat(filepath.Join(projectRoot, c.MarkerDir))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("client: stat %s: %w", c.MarkerDir, err)
		}
		if info.IsDir() {
			found = append(found, c)
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		return registryIndex(found[i].Name) < registryIndex(found[j].Name)
	})
	return found, nil
}

func registryIndex(name string) int {
	for i, c := range Registry {
		if c.Name == name {
			return i
		}
	}
	return len(Registry)
}
