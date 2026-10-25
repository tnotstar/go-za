# go-za

`za` sets up and will manage **private multi-repository AI/development
workspaces**. In such a workspace, one private Git meta-repository holds
private AI-agent context, native Go/uv integration workspaces and public
projects as Git submodules.

`za` replaces the hand-written shell scripts that such workspaces otherwise
need. It is written entirely in Go and builds a single executable, `za`.

## Current scope

| Command | Status |
|---|---|
| `za init` | Initializes an empty directory as a meta-workspace |
| `za project add` | Adds one root-module Go repository as a project (first minimal version) |

`za` creates no worktrees, installs no hooks and never calls GitHub or any
other provider API. Those, and the rest of the project lifecycle (remove,
list, sync, ...), are future subcommands.

## Quick start

```bash
go install github.com/tnotstar/go-za/cmd/za@latest

za init --go --python ./platform
```

To build from a clone instead:

```bash
go build ./cmd/za     # produces ./za
```

## `za init`

```text
za init [--go] [--python] [PATH]
```

| Argument | Meaning |
|---|---|
| `--go` | Create a Go workspace with `go work init` |
| `--python` | Create a uv integration workspace with `uv init` |
| `PATH` | Target directory; defaults to the current directory |

At least one of `--go` and `--python` is required. You can pass both. Flags
may come before or after `PATH`.

```bash
za init --go                      # Go workspace in the current directory
za init --python ~/work/platform  # Python/uv workspace
za init --go --python ../platform # polyglot workspace
```

### Target directory

- If `PATH` exists, it must be a directory with **no entries at all**. Hidden
  files such as `.git` or `.DS_Store` count as entries.
- If `PATH` does not exist, `za init` creates it, including missing parents.
- Running `za init` on an already initialized workspace fails, because the
  directory is not empty. There is no `--force`.

### Required tools

| Tool | Required |
|---|---|
| `git` | always |
| `go` | with `--go` |
| `uv` | with `--python` |

`za` checks for all required tools before it touches the filesystem. It runs
them directly, never through a shell:

```text
git init --quiet
GOWORK=off go work init
uv init --bare --vcs none --author-from none --no-workspace \
        --no-python-downloads --no-config --name <name> <PATH>
```

`<name>` is the directory name normalized to a valid Python project name. For
example, `My Workspace` becomes `my-workspace`.

`za init --python` never downloads Python. If uv finds no suitable local
interpreter, initialization fails with uv's error. Install Python yourself
first. `--no-config` stops uv from reading `uv.toml` or other uv configuration
files, so user- and machine-level uv settings cannot change what gets
generated. `UV_*` environment variables still apply.

After `uv init`, `za` appends the tables that turn the root into a private,
non-package integration workspace:

```toml
[tool.uv]
package = false

[tool.uv.workspace]
members = []
exclude = [".worktrees/**"]
```

`za` keeps any other `[tool.*]` tables that uv generates. If uv's output
already defines anything under `tool.uv`, `za` refuses to overwrite it and
rolls back.

### Exit status

| Code | Meaning |
|---|---|
| `0` | Success (summary on stdout) |
| `1` | Operational failure: non-empty target, missing tool, tool error |
| `2` | Invalid command-line usage |

If a step fails after initialization has started, `za` removes the entries it
created and any directories it made. It never removes entries it did not
create; it reports them and leaves them in place.

## `za project add`

```text
za project add <repo-url>
```

Adds the repository at `<repo-url>` to the enclosing workspace as a public
project. This is the **first, minimal implementation**: it registers one
project and is not yet a general project lifecycle manager.

```bash
za project add https://github.com/tnotstar/go-za.git
za project add git@github.com:tnotstar/go-za.git
```

Both produce the project ID `go-za`: the last component of the URL, without
trailing slashes and one trailing `.git`. The ID is never rewritten into a
different name. It must be a single path component of ASCII letters, digits,
`.`, `_` and `-` that does not start with `.` or `-`; otherwise the command
fails. There is no `--name`, `--branch` or `--force` yet.

### Requirements

- Run it from the workspace root or any directory below it. The nearest
  `za.toml` above the current directory identifies the workspace; `go.work`,
  `.agents/` or `.git/` alone never do.
- `za.toml` must have `schema = 1` and `workspace.go = true`. Only **Go**
  projects are supported, and the module must be at the repository root
  (`<project-id>/go.mod`). Repositories with nested modules only are rejected.
- The workspace must be the top level of a Git working tree with at least
  one commit (commit the `za init` bootstrap first) and **no staged, unstaged
  or untracked changes**. `za` never stashes, discards or commits for you.
- `<project-id>/`, `.agents/context/projects/<project-id>/`,
  `.worktrees/<project-id>/` and a `.gitmodules` entry with that name or path
  must not exist yet.
- `git` and `go` must be in `PATH`.

### What it does

```text
git submodule add -- <repo-url> <project-id>
GOWORK=<workspace>/go.work go work use ./<project-id>
```

1. Adds the repository as the top-level submodule `<project-id>/`. No branch
   is guessed or configured; Git follows the remote `HEAD`.
2. Verifies `<project-id>/go.mod`. It never runs `go mod init`, `go mod tidy`
   or `go get`, and never builds or tests the project.
3. Adds the module to the root `go.work` with `go work use`. The Go toolchain
   owns the result, for example an updated `go` directive.
4. Creates the private context `.agents/context/projects/<project-id>/` from
   the workspace's own templates in `.agents/context/_global/templates/project/`,
   with an empty `decisions/`. Only known facts are filled in: the project ID
   and paths, Go as language, the root `go.work` membership, the bootstrap
   phase and the exact gitlink SHA in `status.md`. Everything else stays a
   `<discover: ...>` or `<decide: ...>` placeholder; no constraints or ADRs
   are invented.
5. Adds the project route to the region between `<!-- za:projects:start -->`
   and `<!-- za:projects:end -->` in `AGENTS.md`, sorted by project ID. The
   rest of the file is not touched. Missing, duplicated or hand-edited markers
   make the command fail.
6. Stages exactly these changes (`.gitmodules`, the gitlink, `go.work`,
   `go.work.sum` if Go creates it, `AGENTS.md` and the new context) and
   **never commits**. Review them with `git diff --cached`.

`za project add` **never writes to the public project**. If the repository
has no `openspec/` yet, the command still succeeds: the private context marks
the functional specification as *OpenSpec bootstrap pending* instead of
creating `openspec/` or a private substitute for it.

`za.toml` is not modified. Project membership lives in `.gitmodules`,
`go.work`, `.agents/context/projects/` and the `AGENTS.md` routing region.

Only Git contacts the repository, using your normal Git configuration and
credentials. `za` cannot prove that a repository is public; it assumes the
URL you pass is appropriate for a public project.

### Failure and rollback

If any step fails or the command is interrupted, `za` rolls the workspace
back to the clean committed state it started from: it resets the index,
removes the submodule checkout, its `.git/modules/<project-id>` directory and
its `.git/config` section, deletes the new context, and restores `AGENTS.md`,
`go.work` and `.gitmodules` byte for byte. A failed addition can be retried
with the same command. If rollback cannot complete, `za` says what is left.

Exit status follows `za init`: `0` success, `1` operational failure, `2`
invalid usage.

## Generated workspace

```text
<workspace>/
├── .git/                    git init
├── za.toml                  za workspace manifest
├── .gitignore               ignores .worktrees/, .agents/runtime/, .venv/
├── .gitattributes           LF line endings
├── .editorconfig
├── AGENTS.md                agent router, with a za-managed project region
├── CLAUDE.md                points Claude Code to AGENTS.md
├── go.work                  --go only, written by the Go toolchain
├── pyproject.toml           --python only, written by uv, then extended
├── .agents/
│   ├── context/
│   │   ├── _global/         GLOBAL context: policy, principles, tooling,
│   │   │   │                roles, glossary, go/python standards
│   │   │   └── templates/project/   PROJECT context templates
│   │   └── projects/        PROJECT contexts (empty until projects exist)
│   ├── hooks/{meta,public}/ reserved for future hook management
│   └── runtime/             local, ignored
└── .worktrees/              local, ignored
```

The context has exactly two levels. GLOBAL context under `_global/` is shared
by all projects. PROJECT context under `projects/<id>/` is private to one
project and inherits from GLOBAL. Language standards are generated only for
the languages you select.

> **`.worktrees/` is ignored for hygiene, not confidentiality.** Private
> context stays private because it lives in the private meta-repository,
> outside every public project's Git working tree. The workspace stays
> correct even if the ignore rules are removed.

### Workspace manifest

Every initialized workspace has a `za.toml` at its root. `za` writes it, and it
is tracked in the meta-repository:

```toml
schema = 1

[workspace]
go = true
python = false
```

| Key | Meaning |
|---|---|
| `schema` | Version of the workspace layout. It is independent of the `za` release version. |
| `workspace.go` | The workspace was initialized with `--go` |
| `workspace.python` | The workspace was initialized with `--python` |

The manifest belongs to `za`, not to the agent context under `.agents/`. It
only marks the directory as a `za` workspace. Projects, paths and tool
versions are deliberately left out, because they have their own sources of
truth.

In `AGENTS.md`, the region between `<!-- za:projects:start -->` and
`<!-- za:projects:end -->` is maintained by `za project add`. Keep your own
edits outside it.

## Development

```bash
make check         # gofmt check, go vet, unit tests, build
make integration   # run against the real git, go and uv
make golden        # regenerate scaffold golden files after template edits
make help          # list every target
```

The Makefile needs GNU Make and a POSIX shell; on Windows, use the shell
shipped with Git for Windows. Every target wraps a plain `go` command, so you
can also run those directly.

The default test suite needs neither `uv` nor network access. Most tests use
a fake command runner; the `za project add` tests also run the real `git` and
`go` against local repositories with an isolated Git configuration. `go test
-short` skips those.

The repository forces LF line endings through `.gitattributes`. Templates are
embedded byte-for-byte and golden tests compare exact bytes, so do not
override this with `core.autocrlf` settings.

## License

[BSD 3-Clause](LICENSE).
