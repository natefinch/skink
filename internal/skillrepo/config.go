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

// Import is one external skills source declared in a skink config file.
// Each Import identifies a git repo (via URL) and optionally narrows the
// set of skills to pick up via Dirs.
//
// Each entry in Dirs accepts these forms:
//
//   - "" or "*": every top-level directory of the repo is a skill (default)
//   - "some/dir": a single skill directory at that path, with optional
//     trailing "/"
//   - "some/dir/*": every immediate subdirectory of "some/dir" is a skill
//
// Version optionally pins the clone to a specific git ref.
type Import struct {
	URL     string   `yaml:"url"     json:"url"     toml:"url"`
	Dirs    []string `yaml:"dirs"    json:"dirs"    toml:"dirs"`
	Version string   `yaml:"version" json:"version" toml:"version"`
}

// Config is the top-level skink config file format.
type Config struct {
	Imports []Import `yaml:"imports" json:"imports" toml:"imports"`
}

// DirSelector is the parsed form of one Import.Dirs entry.
//
//	Prefix       Wildcard  meaning
//	""           true      all top-level dirs of the repo are skills
//	"some/dir"   false     one skill: <repo>/some/dir
//	"some/dir"   true      every subdir of <repo>/some/dir is a skill
type DirSelector struct {
	Prefix   string
	Wildcard bool
}

// ParseDir normalizes one raw dir string from an Import.
func ParseDir(raw string) (DirSelector, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "*" || s == "/" {
		return DirSelector{Wildcard: true}, nil
	}
	// strip leading slash (we don't allow absolute, but be forgiving)
	s = strings.TrimLeft(s, "/")
	wildcard := false
	if strings.HasSuffix(s, "/*") {
		wildcard = true
		s = strings.TrimSuffix(s, "/*")
	} else if strings.HasSuffix(s, "/") {
		s = strings.TrimSuffix(s, "/")
	}
	if s == "" {
		return DirSelector{Wildcard: true}, nil
	}
	if strings.Contains(s, "*") {
		return DirSelector{}, fmt.Errorf("dir %q: '*' may only appear as the final path segment", raw)
	}
	clean := path.Clean(s)
	if clean != s || strings.HasPrefix(clean, "..") || clean == "." {
		return DirSelector{}, fmt.Errorf("dir %q is not a clean relative path", raw)
	}
	return DirSelector{Prefix: clean, Wildcard: wildcard}, nil
}

// configFileNames is the ordered list of accepted skink config filenames in
// the root of a skills repo. If more than one exists, the first match wins.
var configFileNames = []string{
	"skink.yaml",
	"skink.yml",
	"skink.json",
	"skink.toml",
}

// ReadImports reads the skink config from the root of repoDir and returns
// the normalized config. Returns a zero Config if no config file is present.
func ReadImports(repoDir string) (Config, error) {
	var cfg Config
	var found string
	for _, name := range configFileNames {
		p := filepath.Join(repoDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Config{}, fmt.Errorf("skillrepo: read %s: %w", p, err)
		}
		switch filepath.Ext(name) {
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				return Config{}, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		case ".json":
			if err := json.Unmarshal(b, &cfg); err != nil {
				return Config{}, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		case ".toml":
			if err := toml.Unmarshal(b, &cfg); err != nil {
				return Config{}, fmt.Errorf("skillrepo: parse %s: %w", p, err)
			}
		}
		found = p
		break
	}
	if found == "" {
		return Config{}, nil
	}
	out := make([]Import, 0, len(cfg.Imports))
	for i, imp := range cfg.Imports {
		if strings.TrimSpace(imp.URL) == "" {
			return Config{}, fmt.Errorf("skillrepo: %s: imports[%d]: url is required", found, i)
		}
		if _, err := ParseGitURL(imp.URL); err != nil {
			return Config{}, fmt.Errorf("skillrepo: %s: imports[%d]: %w", found, i, err)
		}
		for j, dir := range importDirs(imp) {
			if _, err := ParseDir(dir); err != nil {
				return Config{}, fmt.Errorf("skillrepo: %s: imports[%d].dirs[%d]: %w", found, i, j, err)
			}
		}
		out = append(out, imp)
	}
	cfg.Imports = out
	return cfg, nil
}

func importDirs(imp Import) []string {
	if len(imp.Dirs) == 0 {
		return []string{""}
	}
	return imp.Dirs
}

// GitURL is a parsed git clone URL, split into the components skink uses
// to decide where to clone the repo and lay out skills on disk.
type GitURL struct {
	// Host is the server hostname ("github.com", "example.com", ...).
	Host string
	// Path is the repo path on the server with leading/trailing "/" and a
	// trailing ".git" stripped. E.g. "my-org/my-repo".
	Path string
	// Original is the URL as the user wrote it; this is what skink
	// passes to `git clone` so the user's chosen protocol and credentials
	// keep working.
	Original string
}

// ParseGitURL accepts any of the common git URL forms and extracts Host +
// Path. Supported shapes:
//
//   - https://HOST/PATH
//   - http://HOST/PATH
//   - ssh://[USER@]HOST[:PORT]/PATH
//   - git://HOST/PATH
//   - [USER@]HOST:PATH            (scp-like)
//   - HOST/PATH                   (bare, implicit scheme)
//
// A trailing ".git" on PATH is stripped. Trailing slashes are trimmed.
func ParseGitURL(raw string) (GitURL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return GitURL{}, errors.New("empty git URL")
	}
	orig := s

	var host, rest string
	switch {
	case hasScheme(s, "https://"), hasScheme(s, "http://"),
		hasScheme(s, "ssh://"), hasScheme(s, "git://"),
		hasScheme(s, "git+ssh://"):
		i := strings.Index(s, "://")
		afterScheme := s[i+3:]
		// Strip optional user@ before host.
		if slash := strings.Index(afterScheme, "/"); slash >= 0 {
			hostpart := afterScheme[:slash]
			rest = afterScheme[slash+1:]
			if at := strings.LastIndex(hostpart, "@"); at >= 0 {
				hostpart = hostpart[at+1:]
			}
			if colon := strings.Index(hostpart, ":"); colon >= 0 {
				hostpart = hostpart[:colon]
			}
			host = hostpart
		} else {
			return GitURL{}, fmt.Errorf("git URL %q has no path", raw)
		}
	case strings.Contains(s, "://"):
		return GitURL{}, fmt.Errorf("git URL %q uses unsupported scheme", raw)
	default:
		// scp-like or bare. scp form: [user@]host:path where ':' precedes any '/'.
		firstColon := strings.Index(s, ":")
		firstSlash := strings.Index(s, "/")
		if firstColon >= 0 && (firstSlash < 0 || firstColon < firstSlash) {
			hostpart := s[:firstColon]
			rest = s[firstColon+1:]
			if at := strings.LastIndex(hostpart, "@"); at >= 0 {
				hostpart = hostpart[at+1:]
			}
			host = hostpart
		} else {
			if firstSlash < 0 {
				return GitURL{}, fmt.Errorf("git URL %q has no path", raw)
			}
			host = s[:firstSlash]
			rest = s[firstSlash+1:]
		}
	}

	host = strings.TrimSpace(host)
	rest = strings.Trim(rest, "/")
	rest = strings.TrimSuffix(rest, ".git")
	rest = strings.TrimRight(rest, "/")

	if host == "" {
		return GitURL{}, fmt.Errorf("git URL %q has no host", raw)
	}
	if rest == "" {
		return GitURL{}, fmt.Errorf("git URL %q has no path", raw)
	}
	if strings.Contains(rest, "..") {
		return GitURL{}, fmt.Errorf("git URL %q has invalid path", raw)
	}
	return GitURL{Host: host, Path: rest, Original: orig}, nil
}

func hasScheme(s, scheme string) bool {
	return len(s) >= len(scheme) && strings.EqualFold(s[:len(scheme)], scheme)
}

// CloneURL returns a URL suitable for `git clone`. If the user wrote a
// bare "host/path" form (no scheme, no scp-style colon), we synthesize
// "https://host/path" so git can resolve it; otherwise the original URL
// is passed through unchanged so the user's chosen protocol, auth, and
// credentials keep working.
func (g GitURL) CloneURL() string {
	s := strings.TrimSpace(g.Original)
	if strings.Contains(s, "://") {
		return g.Original
	}
	firstColon := strings.Index(s, ":")
	firstSlash := strings.Index(s, "/")
	if firstColon >= 0 && (firstSlash < 0 || firstColon < firstSlash) {
		// scp-style; pass through.
		return g.Original
	}
	return "https://" + g.Host + "/" + g.Path + ".git"
}

// CloneDirSegments returns the path segments that identify this repo on
// disk under ~/.skink and under a client's skills directory: the host
// followed by each path segment.
func (g GitURL) CloneDirSegments() []string {
	segs := []string{g.Host}
	for _, p := range strings.Split(g.Path, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	return segs
}

// DisplayPath returns "host/path" — a compact identifier used in messages
// and as the Source label on listed skills.
func (g GitURL) DisplayPath() string {
	return g.Host + "/" + g.Path
}
