# go-za

`za` sets up and will manage **private multi-repository AI/development
workspaces**. In such a workspace, one private Git meta-repository holds
private AI-agent context, native Go/uv integration workspaces and
independent project repositories as Git submodules.

`za` replaces the hand-written shell scripts that such workspaces otherwise
need. It is written entirely in Go and builds a single executable, `za`.

Registration as a submodule says nothing about a repository's visibility;
projects may be public, client-private or otherwise restricted.

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
za init [--go] [--python] [--rust] [PATH]
```

| Argument | Meaning |
|---|---|
| `--go` | Create a Go workspace with `go work init` |
| `--python` | Create a uv integration workspace with `uv init` |
| `--rust` | Create a root Cargo workspace scaffold (virtual manifest, no members) |
| `PATH` | Target directory; defaults to the current directory |

At least one of `--go`, `--python` and `--rust` is required. You can pass any
combination. Flags may come before or after `PATH`.

```bash
za init --go                      # Go workspace in the current directory
za init --python ~/work/platform  # Python/uv workspace
za init --rust work-on-rust       # Rust workspace scaffold
za init --go --rust work-on-go-rust
za init --python --rust work-on-python-rust
za init --go --python --rust work-on-polyglot
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
| `cargo` and `rustc` | with `--rust` (version 1.85.0 or newer) |

`za` checks for all required tools before it touches the filesystem. It runs
them directly, never through a shell:

```text
git init --quiet
GOWORK=off go work init
uv init --bare --vcs none --author-from none --no-workspace \
        --no-python-downloads --no-config --name <name> <PATH>
cargo metadata --format-version 1 --no-deps --offline \
        --manifest-path <PATH>/Cargo.toml
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

### Rust workspace

`za init --rust` writes a deterministic, **virtual** root `Cargo.toml` — no
root package, no source directory, no dummy crate, no `rust-toolchain.toml`:

```toml
[workspace]
members = []
exclude = [".worktrees"]
resolver = "3"
```

`za` creates this scaffold directly; it never runs `cargo new` or `cargo
init`. It then proves the scaffold with the installed Cargo: `cargo metadata
--format-version 1 --no-deps --offline` must resolve `workspace_root` to the
new workspace, report zero members and zero packages, and work fully offline.
A workspace with no members is the valid empty bootstrap state — there is
nothing to `cargo check` or `cargo test` yet, and `za` never adds a synthetic
member to make the workspace compile.

The supported toolchain floor is **1.85.0** for both `cargo` and `rustc` —
the explicit minimum for resolver 3 and Rust 2024 support. `za` checks the
effective installed executables and fails before touching the filesystem if
they are missing or older. It never installs, updates or downloads
toolchains: all Cargo invocations set `RUSTUP_AUTO_INSTALL=0`, and when
rustup manages the installation, `za` pins `RUSTUP_TOOLCHAIN` to the
toolchain resolved at preflight so ambient `rust-toolchain.toml` files or
environment overrides cannot redirect — and thereby silently download — a
different toolchain during initialization. If the active toolchain selection
is not installed, `za` fails with an actionable error instead.

Ownership and reproducibility:

- The root `Cargo.toml` is private meta-repository content and versioned.
- The root `/target/` directory is ignored for hygiene. Cargo configuration
  may redirect output elsewhere; the ignore rule is not a boundary.
- A root `Cargo.lock` is **not** ignored. The metadata validation creates
  none for the empty workspace; if a later, legitimate Cargo operation
  creates one, it stays versioned. `za` never writes a lockfile by hand.
- Future project members keep their own manifests and lockfile policies.
  Once a package is a workspace member, Cargo uses the workspace lockfile
  for that invocation, regardless of the project's own on-disk lockfile.

Rust project onboarding (registering Cargo members, existing-workspace
handling, dependency wiring) is future work. Registering a project with
`za project add` does not touch Cargo membership, and `exclude =
[".worktrees"]` keeps task worktrees out of the integration workspace —
though exclusion from membership does not isolate ancestor Cargo
configuration; see `rust-standards.md` in a generated workspace.

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

Adds the repository at `<repo-url>` to the enclosing workspace as a project.
This is the **first, minimal implementation**: it registers one root-module
Go project and is not yet a general project lifecycle manager. `za` neither
knows nor changes a repository's visibility; it registers whatever URL you
pass.

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
- `za.toml` must have `schema = 3` and `workspace.go = true`. Only **Go**
  projects are supported, and the module must be at the repository root
  (`<project-id>/go.mod`). Repositories with nested modules only are rejected.
- The workspace must be the top level of a Git working tree with at least
  one commit (commit the `za init` bootstrap first) and **no staged, unstaged
  or untracked changes**. `za` never stashes, discards or commits for you.
- `<project-id>/`, `.agents/context/<project-id>/`,
  `.worktrees/<project-id>/` and a `.gitmodules` entry with that name or path
  must not exist yet. Project IDs must not collide with reserved workspace or
  context names, including case-equivalent variants (so `issues`,
  `templates`, `policy.md` or `Policy.md` are all rejected).
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
4. Creates the private context `.agents/context/<project-id>/` from the
   workspace's own templates in `.agents/context/templates/project/`, with an
   empty `decisions/`. Only known facts are filled in: the project ID and
   paths, Go as language, the root `go.work` membership, the bootstrap phase,
   the date the pin was observed and the exact gitlink SHA as `Pinned
   gitlink` in `status.md`. Registering does **not** mark the context as
   verified: `status.md` records `Context verified: pending` until someone
   actually reviews the context against that revision. Everything else stays
   a `<discover: ...>` or `<decide: ...>` placeholder; no constraints or ADRs
   are invented.
5. Adds the project route to the region between `<!-- za:projects:start -->`
   and `<!-- za:projects:end -->` in `AGENTS.md`, sorted by project ID. The
   rest of the file is not touched. Missing, duplicated or hand-edited markers
   make the command fail.
6. Stages exactly these changes (`.gitmodules`, the gitlink, `go.work`,
   `go.work.sum` if Go creates it, `AGENTS.md` and the new context) and
   **never commits**. Review them with `git diff --cached`.

`za project add` **never writes to the project repository**. OpenSpec is
optional: if `<project-id>/openspec/` exists, the private context records it
as a *detected* requirement authority to confirm, not as accepted fact; if
not, the requirement authority is recorded as `unresolved`. Either way the
command never creates `openspec/` or a private substitute specification.

`za.toml` is not modified. Project membership lives in `.gitmodules`,
`go.work`, `.agents/context/<project-id>/` and the `AGENTS.md` routing region.

Only Git contacts the repository, using your normal Git configuration and
credentials.

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
├── za.toml                  za workspace manifest (schema = 3)
├── .gitignore               ignores .worktrees/, .agents/runtime/, .venv/
├── .gitattributes           LF line endings
├── .editorconfig
├── AGENTS.md                agent router, with a za-managed project region
├── CLAUDE.md                points Claude Code to AGENTS.md
├── go.work                  --go only, written by the Go toolchain
├── pyproject.toml           --python only, written by uv, then extended
├── Cargo.toml               --rust only: virtual workspace, no members
├── .agents/
│   ├── context/
│   │   ├── INDEX.md         context router (GLOBAL ↔ PROJECT ↔ issues)
│   │   ├── policy.md        GLOBAL context: policy, principles, tooling,
│   │   │                    roles, glossary, go/python/rust standards
│   │   ├── templates/project/   PROJECT context templates
│   │   ├── templates/issue.md   issue template (issue commands come later)
│   │   └── <project-id>/    PROJECT contexts (created by za project add)
│   ├── hooks/{meta,public}/ reserved for future hook management
│   └── runtime/             local, ignored
└── .worktrees/              local, ignored
```

The context has exactly two levels, both direct children of
`.agents/context/`: GLOBAL context files, shared by all projects, and
PROJECT context under `<project-id>/`, private to one project and inheriting
from GLOBAL. `.agents/context/INDEX.md` is the router between them; it never
duplicates the project list (that is derived from `.gitmodules`) and never
embeds issue bodies. Language standards are generated only for the languages
you select (`go-standards.md`, `python-standards.md`, `rust-standards.md`),
but `rust-standards.md` is a reserved name in every workspace. Project IDs
must not collide with context infrastructure names; `za project add`
rejects reserved names and case-equivalent collisions.

> **`.worktrees/` is ignored for hygiene, not confidentiality.** Private
> context stays private because it lives in the private meta-repository,
> outside every public project's Git working tree. The workspace stays
> correct even if the ignore rules are removed.

### Workspace manifest

Every initialized workspace has a `za.toml` at its root. `za` writes it, and it
is tracked in the meta-repository:

```toml
schema = 3

[workspace]
go = true
python = false
rust = false
```

| Key | Meaning |
|---|---|
| `schema` | Version of the workspace layout. It is independent of the `za` release version. |
| `workspace.go` | The workspace was initialized with `--go` |
| `workspace.python` | The workspace was initialized with `--python` |
| `workspace.rust` | The workspace was initialized with `--rust` |

`za` always writes all three capability booleans. Reading a schema-3
manifest without `workspace.rust` (written by an earlier schema-3 release)
means `rust = false`; invalid types are rejected.

The manifest belongs to `za`, not to the agent context under `.agents/`. It
only marks the directory as a `za` workspace. Projects, paths and tool
versions are deliberately left out, because they have their own sources of
truth.

In `AGENTS.md`, the region between `<!-- za:projects:start -->` and
`<!-- za:projects:end -->` is maintained by `za project add`. Keep your own
edits outside it.

### Schemas and compatibility

Schema 3 is the flat context layout: GLOBAL files and project context
directories are direct children of `.agents/context/`, `.agents/context/INDEX.md`
routes between them, OpenSpec is optional per project, and requirement
authorities are explicit. Earlier schemas described different layouts:

| Schema | Layout |
|---|---|
| 1 | Nested `.agents/context/_global/` and `.agents/context/projects/` (previous releases of `za`) |
| 2 | Flat layout with mandatory OpenSpec assumptions (a designed intermediate, never shipped) |
| 3 | Flat layout, optional OpenSpec, explicit requirement authority, reserved issue namespace |

**There is no migration.** This version of `za` refuses to mutate schema-1 or
schema-2 workspaces: every command that reads `za.toml` rejects unsupported
schemas instead of guessing, and never partially flattens an old workspace as
a side effect.

- **Disposable workspaces** (test scaffolds, empty bootstraps) can simply be
  deleted and recreated with `za init`.
- **Workspaces holding valuable context** — authored GLOBAL topics, project
  context, decisions — should be preserved as they are until a controlled
  transfer or migration exists that can carry that knowledge and Git state
  over explicitly. Keep using the `za` version that created them for any
  mutation; a future migration must preflight collisions and preserve
  user-authored content, and none is specified today.

Changing only the `schema` line of an existing workspace is **not** a
migration and remains unsupported — it makes the workspace claim a layout it
does not have, and commands will fail on the missing structure (for example
the context templates or `INDEX.md`).

Older `za` binaries likewise reject schema-3 manifests as unsupported instead
of partially handling them. Binaries from before the Rust capability reject a
Rust-enabled manifest outright with `unknown key workspace.rust` — they never
silently ignore the capability or partially apply it. A Rust-enabled
workspace therefore requires a `za` binary with Rust support; there is no
downgrade path, and no schema bump was needed for this additive capability.

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
