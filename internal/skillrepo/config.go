package skillrepo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Import is one external skills repo declared in a skillnk config file.
type Import struct {
	Name string `yaml:"name" json:"name" toml:"name"`
	URL  string `yaml:"url"  json:"url"  toml:"url"`
	// Version is an optional git ref (tag, branch, or commit SHA) to pin
	// the import to. If empty, the import tracks the remote default branch
	// and is updated via git pull --ff-only. When set, skillnk checks out
	// this ref after cloning and re-checks it out (after git fetch) on
	// every update.
	Version string `yaml:"version" json:"version" toml:"version"`
}

type repoConfigFile struct {
	Imports []Import `yaml:"imports" json:"imports" toml:"imports"`
}

// configFileNames is the ordered list of accepted skillnk config filenames in
// the root of a skills repo. If more than one exists, the first match wins.
var configFileNames = []string{
	"skillnk.yaml",
	"skillnk.yml",
	"skillnk.json",
	"skillnk.toml",
}

// reservedImportNames are names an import may not take because they would
// collide with skillnk's own files in ~/.skillnk.
var reservedImportNames = map[string]struct{}{
	"repo":        {},
	"config.yaml": {},
}

// ReadImports reads the skillnk config from the root of repoDir and returns
// the normalized import list. If no config file is present, it returns (nil,
// nil). Imports with a missing Name are defaulted from their URL.
func ReadImports(repoDir string) ([]Import, error) {
	var (
		cfg   repoConfigFile
		found string
	)
	for _, name := range configFileNames {
		p := filepath.Join(repoDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("skillrepo: read %s: %w", p, err)
		}
		switch filepath.Ext(name) {
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				return nil, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		case ".json":
			if err := json.Unmarshal(b, &cfg); err != nil {
				return nil, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		case ".toml":
			if err := toml.Unmarshal(b, &cfg); err != nil {
				return nil, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		}
		found = p
		break
	}
	if found == "" {
		return nil, nil
	}

	seen := map[string]struct{}{}
	out := make([]Import, 0, len(cfg.Imports))
	for i, imp := range cfg.Imports {
		if strings.TrimSpace(imp.URL) == "" {
			return nil, fmt.Errorf("skillrepo: %s: imports[%d]: url is required", found, i)
		}
		if imp.Name == "" {
			imp.Name = DefaultImportName(imp.URL)
		}
		if err := validateImportName(imp.Name); err != nil {
			return nil, fmt.Errorf("skillrepo: %s: imports[%d]: %w", found, i, err)
		}
		if _, dup := seen[imp.Name]; dup {
			return nil, fmt.Errorf("skillrepo: %s: imports[%d]: duplicate name %q", found, i, imp.Name)
		}
		seen[imp.Name] = struct{}{}
		out = append(out, imp)
	}
	return out, nil
}

// DefaultImportName derives an import name from a git URL. It strips common
// github.com prefixes and any trailing ".git". If no github.com prefix is
// present, it falls back to the URL's final path segment.
func DefaultImportName(url string) string {
	s := strings.TrimSpace(url)
	stripped := false
	for _, p := range []string{
		"https://github.com/",
		"http://github.com/",
		"ssh://git@github.com/",
		"git@github.com:",
		"github.com/",
		"github.com:",
	} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			stripped = true
			break
		}
	}
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	if !stripped {
		s = path.Base(s)
		s = strings.TrimSuffix(s, ".git")
	}
	if s == "" || s == "." || s == "/" {
		return "import"
	}
	return s
}

func validateImportName(name string) error {
	if name == "" {
		return errors.New("name is empty")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("name %q must not start with '.'", name)
	}
	if strings.ContainsAny(name, `\`+"\x00") || strings.Contains(name, "..") {
		return fmt.Errorf("name %q contains invalid characters", name)
	}
	// slashes are allowed (e.g. "owner/repo") but reject absolute / parent refs
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("name %q must not be absolute", name)
	}
	if _, bad := reservedImportNames[name]; bad {
		return fmt.Errorf("name %q is reserved", name)
	}
	return nil
}
