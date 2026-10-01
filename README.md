# go-za

`za` sets up and will manage **private multi-repository AI/development
workspaces**. In such a workspace, one private Git meta-repository holds
private AI-agent context, native Go/uv integration workspaces and, later,
public projects as Git submodules.

`za` replaces the hand-written shell scripts that such workspaces otherwise
need. It is written entirely in Go and builds a single executable, `za`.

## Current scope

Only one command exists so far:

| Command | Status |
|---|---|
| `za init` | Initializes an empty directory as a meta-workspace |

`za init` does **not** add projects or Git submodules. It creates no
worktrees, installs no hooks and never contacts GitHub. Those are future
subcommands.

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
uv init --bare --vcs none --author-from none --no-workspace --name <name> <PATH>
```

`<name>` is the directory name normalized to a valid Python project name. For
example, `My Workspace` becomes `my-workspace`.

After `uv init`, `za` appends the tables that turn the root into a private,
non-package integration workspace:

```toml
[tool.uv]
package = false

[tool.uv.workspace]
members = []
exclude = [".worktrees/**"]
```

### Exit status

| Code | Meaning |
|---|---|
| `0` | Success (summary on stdout) |
| `1` | Operational failure: non-empty target, missing tool, tool error |
| `2` | Invalid command-line usage |

If a step fails after initialization has started, `za` removes the entries it
created and any directories it made. It never removes entries it did not
create; it reports them and leaves them in place.

## Generated workspace

```text
<workspace>/
├── .git/                    git init
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

In `AGENTS.md`, the region between `<!-- za:projects:start -->` and
`<!-- za:projects:end -->` is reserved for future `za` commands. Keep your own
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

The default test suite uses a fake command runner. It needs neither `uv` nor
network access.

The repository forces LF line endings through `.gitattributes`. Templates are
embedded byte-for-byte and golden tests compare exact bytes, so do not
override this with `core.autocrlf` settings.

## License

[BSD 3-Clause](LICENSE).
