package skillrepo

import (
	"bytes"
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
	SkillDir string   `yaml:"skilldir" json:"skilldir" toml:"skilldir"`
	Imports  []Import `yaml:"imports"  json:"imports"  toml:"imports"`
}

// ErrConfigNotFound is returned when a project has no skink config file.
var ErrConfigNotFound = errors.New("skillrepo: config not found")

// ErrWildcardRemove is returned when removing one concrete skill would require
// editing a wildcard selector.
var ErrWildcardRemove = errors.New("skillrepo: cannot remove one skill from wildcard import")

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

// NormalizeSkillDir normalizes the top-level skilldir config value.
func NormalizeSkillDir(raw string) (string, error) {
	s := filepath.ToSlash(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return "", fmt.Errorf("skilldir %q is not a clean relative path", raw)
	}
	clean := path.Clean(s)
	if clean != s || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("skilldir %q is not a clean relative path", raw)
	}
	return clean, nil
}

// configFileNames is the ordered list of accepted skink config filenames in
// the root of a project repo. If more than one exists, the first match wins.
var configFileNames = []string{
	".skink.yaml",
	".skink.yml",
	".skink.json",
	".skink.toml",
}

// FindConfig returns the first skink config file found in repoDir according
// to configFileNames precedence.
func FindConfig(repoDir string) (string, bool, error) {
	for _, name := range configFileNames {
		p := filepath.Join(repoDir, name)
		_, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, fmt.Errorf("skillrepo: stat %s: %w", p, err)
		}
		return p, true, nil
	}
	return "", false, nil
}

// ReadImports reads the skink config from the root of repoDir and returns
// the normalized config.
func ReadImports(repoDir string) (Config, error) {
	var cfg Config
	found, ok, err := FindConfig(repoDir)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return Config{}, ErrConfigNotFound
	}
	b, err := os.ReadFile(found)
	if err != nil {
		return Config{}, fmt.Errorf("skillrepo: read %s: %w", found, err)
	}
	switch filepath.Ext(found) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("skillrepo: parse %s: %w", found, err)
		}
	case ".json":
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("skillrepo: parse %s: %w", found, err)
		}
	case ".toml":
		if err := toml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("skillrepo: parse %s: %w", found, err)
		}
	}
	skillDir, err := NormalizeSkillDir(cfg.SkillDir)
	if err != nil {
		return Config{}, fmt.Errorf("skillrepo: %s: %w", found, err)
	}
	cfg.SkillDir = skillDir
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

// SaveConfig writes cfg to the existing skink config file in repoDir, or to
// .skink.yaml if no config exists yet.
func SaveConfig(repoDir string, cfg Config) error {
	path, ok, err := FindConfig(repoDir)
	if err != nil {
		return err
	}
	if !ok {
		path = filepath.Join(repoDir, ".skink.yaml")
	}
	if err := validateConfig(cfg, path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("skillrepo: mkdir config parent: %w", err)
	}

	var b []byte
	switch filepath.Ext(path) {
	case ".yaml", ".yml":
		b, err = yaml.Marshal(cfg)
	case ".json":
		b, err = json.MarshalIndent(cfg, "", "  ")
		if err == nil {
			b = append(b, '\n')
		}
	case ".toml":
		var buf bytes.Buffer
		err = toml.NewEncoder(&buf).Encode(cfg)
		b = buf.Bytes()
	default:
		err = fmt.Errorf("unsupported config extension %q", filepath.Ext(path))
	}
	if err != nil {
		return fmt.Errorf("skillrepo: marshal %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".skink-*.tmp")
	if err != nil {
		return fmt.Errorf("skillrepo: tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("skillrepo: write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("skillrepo: close config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("skillrepo: rename config: %w", err)
	}
	return nil
}

func validateConfig(cfg Config, found string) error {
	if _, err := NormalizeSkillDir(cfg.SkillDir); err != nil {
		return fmt.Errorf("skillrepo: %s: %w", found, err)
	}
	for i, imp := range cfg.Imports {
		if strings.TrimSpace(imp.URL) == "" {
			return fmt.Errorf("skillrepo: %s: imports[%d]: url is required", found, i)
		}
		if _, err := ParseGitURL(imp.URL); err != nil {
			return fmt.Errorf("skillrepo: %s: imports[%d]: %w", found, i, err)
		}
		for j, dir := range importDirs(imp) {
			if _, err := ParseDir(dir); err != nil {
				return fmt.Errorf("skillrepo: %s: imports[%d].dirs[%d]: %w", found, i, j, err)
			}
		}
	}
	return nil
}

// AddImportDirs appends dirs to an unpinned import for url, creating one if
// needed. Existing dirs are not duplicated.
func AddImportDirs(cfg Config, url string, dirs []string) Config {
	existing := map[string]struct{}{}
	for i := range cfg.Imports {
		imp := &cfg.Imports[i]
		if imp.URL != url || imp.Version != "" {
			continue
		}
		for _, d := range imp.Dirs {
			existing[d] = struct{}{}
		}
		for _, d := range dirs {
			if _, ok := existing[d]; ok {
				continue
			}
			imp.Dirs = append(imp.Dirs, d)
			existing[d] = struct{}{}
		}
		return cfg
	}
	cfg.Imports = append(cfg.Imports, Import{URL: url, Dirs: append([]string(nil), dirs...)})
	return cfg
}

// UpdateImportDirs replaces this repo's configured dirs that correspond to
// knownDirs with selectedDirs, preserving unrelated dirs and imports.
func UpdateImportDirs(cfg Config, url string, knownDirs, selectedDirs []string) Config {
	key, ok := repoKey(url)
	if !ok {
		return cfg
	}
	known := map[string]struct{}{}
	for _, dir := range knownDirs {
		known[dir] = struct{}{}
	}
	selected := uniqueStrings(selectedDirs)

	var out []Import
	var target *Import
	for _, imp := range cfg.Imports {
		impKey, ok := repoKey(imp.URL)
		if !ok || impKey != key {
			out = append(out, imp)
			continue
		}
		if target == nil {
			copy := imp
			copy.Dirs = nil
			target = &copy
		}
		for _, dir := range effectiveDirs(imp) {
			if selectorTouchesKnown(dir, known) {
				continue
			}
			target.Dirs = appendUnique(target.Dirs, dir)
		}
	}
	if target == nil {
		target = &Import{URL: url}
	}
	for _, dir := range selected {
		target.Dirs = appendUnique(target.Dirs, dir)
	}
	if len(target.Dirs) > 0 {
		out = append(out, *target)
	}
	cfg.Imports = out
	return cfg
}

// SetRepoVersion sets version for every import matching url. An empty version
// makes matching imports track the default branch again.
func SetRepoVersion(cfg Config, url, version string) Config {
	key, ok := repoKey(url)
	if !ok {
		return cfg
	}
	for i := range cfg.Imports {
		impKey, ok := repoKey(cfg.Imports[i].URL)
		if ok && impKey == key {
			cfg.Imports[i].Version = version
		}
	}
	return cfg
}

// RemoveRepoDir removes one concrete dir from imports matching url. Wildcard
// selectors are not rewritten because the config format has no exclusions.
func RemoveRepoDir(cfg Config, url, dir string) (Config, error) {
	key, ok := repoKey(url)
	if !ok {
		return cfg, nil
	}
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	var out []Import
	for _, imp := range cfg.Imports {
		impKey, ok := repoKey(imp.URL)
		if !ok || impKey != key {
			out = append(out, imp)
			continue
		}
		dirs := effectiveDirs(imp)
		if len(imp.Dirs) == 0 {
			if importIncludesDir(imp, dir) {
				return cfg, ErrWildcardRemove
			}
			out = append(out, imp)
			continue
		}
		next := imp
		next.Dirs = nil
		removed := false
		for _, raw := range dirs {
			sel, err := ParseDir(raw)
			if err != nil {
				return cfg, err
			}
			if sel.Wildcard && importIncludesDir(Import{Dirs: []string{raw}}, dir) {
				return cfg, ErrWildcardRemove
			}
			if !sel.Wildcard && sel.Prefix == dir {
				removed = true
				continue
			}
			next.Dirs = append(next.Dirs, raw)
		}
		if !removed {
			out = append(out, imp)
			continue
		}
		if len(next.Dirs) > 0 {
			out = append(out, next)
		}
	}
	cfg.Imports = out
	return cfg, nil
}

// IncludedDirs returns discovered dirs that are already included by imports
// for url.
func IncludedDirs(cfg Config, url string, discoveredDirs []string) map[string]bool {
	out := map[string]bool{}
	key, ok := repoKey(url)
	if !ok {
		return out
	}
	for _, imp := range cfg.Imports {
		impKey, ok := repoKey(imp.URL)
		if !ok || impKey != key {
			continue
		}
		for _, dir := range discoveredDirs {
			if importIncludesDir(imp, dir) {
				out[dir] = true
			}
		}
	}
	return out
}

func repoKey(url string) (string, bool) {
	u, err := ParseGitURL(url)
	if err != nil {
		return "", false
	}
	return u.DisplayPath(), true
}

func effectiveDirs(imp Import) []string {
	if len(imp.Dirs) == 0 {
		return []string{""}
	}
	return imp.Dirs
}

func selectorTouchesKnown(raw string, known map[string]struct{}) bool {
	for dir := range known {
		imp := Import{Dirs: []string{raw}}
		if importIncludesDir(imp, dir) {
			return true
		}
	}
	return false
}

func importIncludesDir(imp Import, rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	for _, raw := range importDirs(imp) {
		sel, err := ParseDir(raw)
		if err != nil {
			continue
		}
		if !sel.Wildcard {
			if rel == sel.Prefix {
				return true
			}
			continue
		}
		if sel.Prefix == "" {
			if !strings.Contains(rel, "/") {
				return true
			}
			continue
		}
		parent := path.Dir(rel)
		if parent == "." {
			parent = ""
		}
		if parent == sel.Prefix {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func appendUnique(in []string, s string) []string {
	for _, existing := range in {
		if existing == s {
			return in
		}
	}
	return append(in, s)
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
