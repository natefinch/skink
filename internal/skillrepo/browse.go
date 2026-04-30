package skillrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DiscoveredSkill is a skill found by walking a repo for SKILL.md files.
type DiscoveredSkill struct {
	Name        string
	Description string
	RelDir      string
	Path        string
}

// DiscoverSkills finds every directory under repoDir containing a SKILL.md
// file and reads skill metadata from that file.
func DiscoverSkills(repoDir string) ([]DiscoveredSkill, error) {
	var out []DiscoveredSkill
	err := filepath.WalkDir(repoDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		skillFile := filepath.Join(p, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		skill, err := readSkillFile(repoDir, p, skillFile)
		if err != nil {
			return err
		}
		out = append(out, skill)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skillrepo: discover skills in %s: %w", repoDir, err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].RelDir < out[j].RelDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func readSkillFile(repoDir, skillDir, skillFile string) (DiscoveredSkill, error) {
	b, err := os.ReadFile(skillFile)
	if err != nil {
		return DiscoveredSkill{}, fmt.Errorf("skillrepo: read %s: %w", skillFile, err)
	}
	meta := parseSkillMetadata(string(b))
	rel, err := filepath.Rel(repoDir, skillDir)
	if err != nil {
		return DiscoveredSkill{}, err
	}
	rel = filepath.ToSlash(rel)
	if meta.Name == "" {
		meta.Name = filepath.Base(skillDir)
	}
	return DiscoveredSkill{
		Name:        meta.Name,
		Description: meta.Description,
		RelDir:      rel,
		Path:        skillDir,
	}, nil
}

func parseSkillMetadata(body string) skillFrontmatter {
	body = strings.TrimPrefix(body, "\ufeff")
	if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
		rest := body[3:]
		rest = strings.TrimPrefix(rest, "\r\n")
		rest = strings.TrimPrefix(rest, "\n")
		if idx := strings.Index(rest, "\n---"); idx >= 0 {
			raw := rest[:idx]
			var meta skillFrontmatter
			if err := yaml.Unmarshal([]byte(raw), &meta); err == nil {
				meta.Name = strings.TrimSpace(meta.Name)
				meta.Description = strings.TrimSpace(meta.Description)
				return meta
			}
		}
	}
	var meta skillFrontmatter
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if meta.Name == "" && strings.HasPrefix(line, "# ") {
			meta.Name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}
		if meta.Description == "" && line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
			meta.Description = line
		}
		if meta.Name != "" && meta.Description != "" {
			break
		}
	}
	return meta
}
