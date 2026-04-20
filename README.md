# skillnk

A tiny CLI that links skills from your personal skills git repo into the AI
client (Claude, Copilot, Cursor, Codex) a project uses. Skills live in one
place, are version-controlled, and are shared across projects via symlinks.

## Install

```
go install github.com/natefinch/skillnk@latest
```

Requires `git` on your `PATH`.

## First run

On first run, skillnk asks for the URL of the git repo that holds your
skills and clones it into `~/.skillnk/repo`. Config is saved to
`~/.skillnk/config.yaml`.

```
$ skillnk init
```

A "skill" is any top-level directory in that repo (dotfiles and `.github`
are ignored).

## The `skillnk` config file

Your skills repo may include a `skillnk` config file at its root, which
currently configures one thing: **imports** — other git repos whose skills
should be added to your library.

### Location and format

The file lives at the root of your skills repo and may be written in any of
four formats. If multiple exist, this precedence applies (first match wins):

1. `skillnk.yaml`
2. `skillnk.yml`
3. `skillnk.json`
4. `skillnk.toml`

### Schema

One top-level key: `imports`, a list of objects.

| field     | required | notes                                                             |
|-----------|----------|-------------------------------------------------------------------|
| `url`     | yes      | Any git URL `git clone` understands.                              |
| `name`    | no       | Directory name under `~/.skillnk/` for the clone. See defaulting. |
| `version` | no       | Pin to a specific git ref — tag, branch, or commit SHA.           |

If `name` is omitted, skillnk strips the `github.com` prefix (handling the
`https://`, `http://`, `ssh://git@`, `git@...:`, and bare forms) and any
trailing `.git`, giving e.g. `owner/repo`. For non-GitHub URLs, the name
defaults to the repo's basename (`repo` for `https://gitlab.example/team/repo.git`).

If `version` is set, skillnk checks out that ref after cloning and
re-checks it out (after `git fetch --tags`) on every `skillnk update`, so
the import stays pinned even when you update the rest of your library. If
`version` is omitted, the import tracks the remote default branch and is
advanced via `git pull --ff-only` on update.

Names must not contain `..`, start with `.` or `/`, or equal the reserved
values `repo` or `config.yaml`. Duplicate names are rejected.

### Examples

```yaml
# skillnk.yaml
imports:
  - name: team-skills
    url: git@github.com:acme/team-skills.git
    version: v1.4.0                                   # pinned to a tag
  - url: https://github.com/charmbracelet/skills.git  # tracks default branch
```

```json
{
  "imports": [
    { "name": "team-skills", "url": "git@github.com:acme/team-skills.git", "version": "v1.4.0" },
    { "url": "https://github.com/charmbracelet/skills.git" }
  ]
}
```

```toml
# skillnk.toml
[[imports]]
name    = "team-skills"
url     = "git@github.com:acme/team-skills.git"
version = "v1.4.0"

[[imports]]
url = "https://github.com/charmbracelet/skills.git"
```

### Behavior

- Imports are cloned into `~/.skillnk/<name>` on first use (during `install`,
  `list`, or `update`). Pinned imports are checked out to `version` right
  after cloning.
- Their top-level directories appear alongside your own skills in `list` and
  the `install` picker, tagged with the source repo name.
- `skillnk update` runs `git pull --ff-only` on the primary checkout and on
  every unpinned import. For pinned imports, it runs `git fetch --tags`
  followed by `git checkout <version>` so the import stays at the pinned
  ref.
- Imports are **not transitive**: a `skillnk` config inside an imported repo
  is ignored.
- If the same skill name appears in more than one source, the primary repo
  wins, then imports in declaration order.

## Commands

| command     | what it does                                                      |
|-------------|-------------------------------------------------------------------|
| `init`      | Prompt for the skills repo and clone it.                          |
| `install`   | Pick skills (multi-select) and symlink them into the project.     |
| `uninstall` | Remove previously-installed skill symlinks (sources untouched).   |
| `list`      | List available skills; mark which are installed in this project. |
| `status`    | Show installed skills and where they link to.                     |
| `update`    | `git pull --ff-only` in the primary checkout and every import.    |

Non-interactive use:

```
skillnk install --client=claude --skill=foo --skill=bar
skillnk uninstall --client=claude --skill=foo
```

## Client detection

skillnk looks for these marker directories in the project root and installs
into the matching skills dir:

| client  | marker     | install target          |
|---------|------------|-------------------------|
| claude  | `.claude`  | `.claude/skills/<name>` |
| copilot | `.github`  | `.github/skills/<name>` |
| cursor  | `.cursor`  | `.cursor/skills/<name>` |
| codex   | `.codex`   | `.codex/skills/<name>`  |

With zero matches, skillnk prompts. With multiple matches, it prompts with
the subset. `--client` overrides detection.

## Development

```
go test ./...
go build ./...
go vet ./...
```

Layout:

```
internal/
  paths/      resolve home, ~/.skillnk, project root (pure)
  config/     load/save ~/.skillnk/config.yaml (pure)
  client/     client registry + auto-detect (pure)
  skillrepo/  clone/pull/list skills via injected GitRunner (pure)
  installer/  symlink create/remove/status (pure)
  tui/        Bubble Tea models (no core logic)
  cli/        Cobra wiring; only layer that imports tui + core
```

Core packages have no UI/CLI knowledge and are fully unit-tested with
`t.TempDir()` and fakes — no network, no real git.
