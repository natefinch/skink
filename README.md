# skink

A tiny CLI for working with AI-client skills declared by the current project.
Projects commit a `skink` config file that names external skills repositories;
skink clones and updates those sources in a shared local cache.

## Install

```
go install github.com/natefinch/skink@latest
```

Requires `git` on your `PATH`.

## The project `skink` config file

Skink reads a config file from the current project root. The config may declare
where skills should be copied with `skilldir`, and declares skill sources via an
`imports:` list. Each import is one git repo; by default every top-level
directory is treated as a skill, but you can narrow it down to one or more
directories or wildcard subtrees with `dirs:`.

### Location and format

The file lives at the root of the project repo and may be written in any of
four formats. If multiple exist, this precedence applies (first match wins):

1. `.skink.yaml`
2. `.skink.yml`
3. `.skink.json`
4. `.skink.toml`

### Schema

Top-level fields:

| field      | required | notes                                                                                                                                  |
|------------|----------|----------------------------------------------------------------------------------------------------------------------------------------|
| `skilldir` | no       | Repo-relative directory where `sync` copies skills. Leading `/` is treated as relative, so `/skills` means `<repo>/skills`.             |
| `imports`  | no       | List of external skill sources.                                                                                                        |

Each `imports` object accepts:

| field     | required | notes                                                              |
|-----------|----------|--------------------------------------------------------------------|
| `url`     | yes      | Any supported git URL.                                             |
| `dirs`    | no       | Which directories of the repo to treat as skills. Defaults to `*`. |
| `version` | no       | Pin to a specific git ref: tag, branch, or commit SHA.             |

Each `dirs` entry accepts:

- omitted or `*` — every top-level directory of the repo is a skill.
- `some/path` (with optional trailing `/`) — a single skill directory at that
  path.
- `some/path/*` — every immediate subdirectory of `some/path` is a skill.

`..`, absolute paths, and wildcards anywhere other than the final segment are
rejected.

If `skilldir` is unset, `skink sync` autodetects the target from the current
repo's AI-client marker directories:

| client  | marker     | default skilldir  |
|---------|------------|-------------------|
| claude  | `.claude`  | `.claude/skills`  |
| copilot | `.github`  | `.github/skills`  |
| cursor  | `.cursor`  | `.cursor/skills`  |
| codex   | `.codex`   | `.codex/skills`   |

If exactly one marker is found, `sync` uses that target. If no markers or
multiple markers are found, `sync` prompts you to choose a target.

### URL handling and cache layout

Skink accepts the usual git URL forms: `https://`, `http://`, `ssh://`,
`git://`, scp-style `user@host:path`, and bare `host/path`. Bare URLs are
cloned as `https://host/path.git`; other forms are passed through to `git clone`
so existing SSH keys and credential helpers keep working.

External sources are cached under `~/.skink/<host>/<path>`. For example, with
`url: git@example.com:my-org/my-repo` and `dirs: [skills/do-the-thing]`, skink
uses:

- Clone cache: `~/.skink/example.com/my-org/my-repo/`
- Skill path: `~/.skink/example.com/my-org/my-repo/skills/do-the-thing/`

This namespacing keeps skills from different sources from colliding on disk and
makes it obvious where each one came from.

### Behavior

- If multiple imports point at the same repo, the clone is shared. An error is
  raised if they disagree on `version`.
- `skink sync` clones missing sources, updates the local cache, then copies each
  configured skill to `<repo>/<skilldir>/<skill-name>`.
- If the destination skill directory already exists, `sync` verifies that it
  exactly matches the cached skill directory. Matching directories are reported
  as already up to date. Different directories are reported as conflicts and are
  not changed unless `sync -f` is used.
- `sync -f` overwrites conflicting destination skill directories so they exactly
  match the cached source, including removing extra files.
- `skink status` lets you choose skills from configured repos or type a new repo
  URL to add. It shallow-clones new repos into the shared
  `~/.skink/<host>/<path>` cache if needed, scans for `SKILL.md` files, and
  shows an interactive expandable selector. Selected skills are added to the
  current project's config and copied into the repo using the same target
  resolution as `sync`. If the chosen repo is already in the config, matching
  skills start checked; unchecking one removes that skill from the config and
  deletes its copied directory from the local repo.
- Imports are **not transitive**: a `skink` config inside an imported repo is
  ignored.
- There is no user-level personal skills repo or `~/.skink/config.yaml`; the
  project config is the source of truth.

### Examples

```yaml
# .skink.yaml
skilldir: /skills

imports:
  # Pull a single skill at a known path, pinned.
  - url: github.com/anthropics/skills
    dirs:
      - skills/skill-creator
    version: v0.3.0

  # Pull every skill under a subdirectory.
  - url: github.com/anthropics/skills
    dirs:
      - skills/*

  # Pull every top-level directory of a private repo.
  - url: git@example.com:my-org/my-repo
    version: v1.4.0

  # Whole repo, default branch.
  - url: https://github.com/charmbracelet/skills.git
```

```json
{
  "skilldir": "/skills",
  "imports": [
    { "url": "github.com/anthropics/skills", "dirs": ["skills/skill-creator"], "version": "v0.3.0" },
    { "url": "github.com/anthropics/skills", "dirs": ["skills/*"] },
    { "url": "git@example.com:my-org/my-repo", "version": "v1.4.0" },
    { "url": "https://github.com/charmbracelet/skills.git" }
  ]
}
```

```toml
# .skink.toml
skilldir = "/skills"

[[imports]]
url     = "github.com/anthropics/skills"
dirs    = ["skills/skill-creator"]
version = "v0.3.0"

[[imports]]
url = "github.com/anthropics/skills"
dirs = ["skills/*"]

[[imports]]
url     = "git@example.com:my-org/my-repo"
version = "v1.4.0"

[[imports]]
url = "https://github.com/charmbracelet/skills.git"
```

### Browsing skills

From `skink status`, press `a` on a repo row to type a new repo URL and choose
skills from it, or press `c` to choose/change skills from that configured repo.
The browse UI discovers skills by finding directories that contain `SKILL.md`.
It reads `name` and `description` from YAML frontmatter:

```markdown
---
name: skill-creator
description: Helps create new skills.
---
```

The browse UI lists the skill name and path relative to the source repo root.
Use right arrow to expand a row and show the description, left arrow to
collapse, space to select, and enter to add the selected skills. If you changed
the checked skills, skink asks whether to discard those changes before leaving
the skill list.

When you confirm, skink appends the selected repo-relative directories to the
current project's skink config and immediately copies those selected skill
directories into the resolved `skilldir`.

If you browse a repo already listed in the config, skills that are already
included start checked. Leaving them checked keeps them configured. Unchecking
them removes those dirs from the config and removes their copied directories
from the local `skilldir`.

### Checking local status

`skink status` opens an interactive status page. It reads the current repo's
skink config, groups configured skills by source repo, and compares each source
skill with the directory that should exist under `skilldir`.

Each skill row shows a status emoji:

| emoji | meaning |
|-------|---------|
| ✅    | the local copy is up to date |
| ⚠️    | the local copy differs from the cached source |
| ❌    | the local copy is missing |

Repo rows show an upgrade emoji when skink sees an available update. Pinned
repos use git tags only: semver-like tags are compared as versions, while
non-semver tags are shown with their tag creation dates for manual selection.
Unpinned repos show an upgrade when remote HEAD differs from the cached
checkout.

Status key bindings:

| key | when | action |
|-----|------|--------|
| `a` | repo row | type a repo URL and choose skills to add from it |
| `c` | repo row | choose/change skills from that repo |
| `t` | repo row | choose a tag to change the repo version |
| `u` | repo row | update pinned repos to the newest semver tag, or pull HEAD for unpinned repos |
| `s` | skill row | sync just that skill directory, overwriting local differences |
| `d` | skill row | confirm delete, remove the copied directory, and remove the dir from config |

Deleting a skill that is included only by a wildcard import is blocked because
the config format has no exclusion syntax. The status page stays open after
changes and refreshes statuses in place.

## Commands

| command  | what it does                                                       |
|----------|--------------------------------------------------------------------|
| `status` | Open an interactive status page for configured skills.             |
| `sync`   | Update configured sources and copy skills into the repo skilldir.  |

Use `sync -f` to force overwrite destination skill directories that differ from the
cached source.

## Development

```
go test ./...
go build ./...
go vet ./...
```

Layout:

```
internal/
  paths/      resolve home, ~/.skink cache, project root (pure)
  client/     client registry + auto-detect (pure)
  skillrepo/  parse project config, clone/pull/list skills via GitRunner
  syncer/     compare and copy skill directories into a project
  installer/  symlink create/remove/status (pure)
  tui/        Bubble Tea models
  cli/        Cobra wiring; only layer that imports core packages
```

Core packages have no UI/CLI knowledge and are fully unit-tested with
`t.TempDir()` and fakes: no network, no real git.
