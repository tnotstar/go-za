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
| `za project add` | Adds one root-module Go repository or one single-root Rust crate as a project |
| `za issue` | Manages the workspace issue records in `.za/issues/` and assembles agent prompts |
| `za board` | Renders the Kanban view of the workspace issues |
| `za wiki` | Manages the workspace wiki pages in `.za/wiki/` |
| `za version` | Prints the version of the executable and its build provenance |

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

Rust project onboarding through `za project add` covers **single-root
crates**: a repository whose root `Cargo.toml` declares `[package]` without
`[workspace]` joins the root workspace `members` array and is verified
offline with `cargo metadata --no-deps`. Repositories that are themselves
Cargo workspaces (virtual manifests, composite package-plus-workspace,
multi-crate monorepos) are rejected with a diagnostic and a clean rollback:
Cargo itself forbids nested workspace roots, so they cannot be members.
Dependency wiring, existing-workspace adoption beyond `za init` scaffolds,
and Python onboarding remain future work. Registering any project never
touches Cargo membership of unrelated workspaces, and `exclude =
[".worktrees"]` keeps task worktrees out of the integration workspace —
though exclusion from membership does not isolate ancestor Cargo
configuration; see `tooling.md` in a generated workspace.

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
It registers **Go** and **Rust** repositories and is not yet a general
project lifecycle manager. `za` neither knows nor changes a repository's
visibility; it registers whatever URL you pass.

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
- `za.toml` must have `schema = 4` and `workspace.go = true` and/or
  `workspace.rust = true`. The project language is detected after cloning:
  a **Go** project carries its module at the repository root
  (`<project-id>/go.mod`; repositories with nested modules only are
  rejected), and a **Rust** project must be a *single root crate*: a
  `<project-id>/Cargo.toml` with a `[package]` table, no `[workspace]`
  table, and no nested `Cargo.toml` anywhere below the root. Cargo rejects
  nested workspace roots, so a repository that is itself a Cargo workspace
  (virtual manifest or composite package-plus-workspace) cannot join the
  root workspace and is rejected with a diagnostic; a repository with both
  `go.mod` and `Cargo.toml` at its root is rejected as ambiguous, and one
  with neither is rejected naming both probes.
- The workspace must be the top level of a Git working tree with at least
  one commit (commit the `za init` bootstrap first) and **no staged, unstaged
  or untracked changes**. `za` never stashes, discards or commits for you.
- `<project-id>/`, `.agents/context/<project-id>/`,
  `.worktrees/<project-id>/` and a `.gitmodules` entry with that name or path
  must not exist yet. Project IDs must not collide with reserved workspace or
  context names, including case-equivalent variants (so `issues`,
  `templates`, `policy.md` or `Policy.md` are all rejected).
- `git` must be in `PATH`, plus `go` for a Go project and `cargo`/`rustc`
  (1.85.0 or newer) for a Rust project. In a Go-enabled workspace the root
  `go.work` must exist; in a Rust-enabled workspace the root `Cargo.toml`
  must carry a `[workspace]` table. The Rust toolchain preflight follows
  `za init`: fully offline, `RUSTUP_AUTO_INSTALL=0`, and `RUSTUP_TOOLCHAIN`
  pinned to the preflight selection when rustup manages the installation.

### What it does

```text
git submodule add -- <repo-url> <project-id>

# Go project:
GOWORK=<workspace>/go.work go work use ./<project-id>

# Rust project (deterministic textual insert, lexically sorted):
<workspace>/Cargo.toml [workspace] members += "<project-id>"
cargo metadata --format-version 1 --no-deps --offline --manifest-path <workspace>/Cargo.toml
```

1. Adds the repository as the top-level submodule `<project-id>/`. No branch
   is guessed or configured; Git follows the remote `HEAD`.
2. Detects the language and validates the root manifest: `<project-id>/go.mod`
   for Go, or the single-root-crate `Cargo.toml` for Rust. It never runs
   `go mod init`, `go mod tidy`, `go get`, `cargo new` or `cargo add`, and
   never builds or tests the project.
3. Registers the project with its workspace: the Go module joins the root
   `go.work` through `go work use` (the Go toolchain owns the result, for
   example an updated `go` directive); the Rust crate is inserted into the
   root `Cargo.toml` `[workspace]` `members` array by a deterministic textual
   edit that keeps the array sorted and preserves comments and formatting
   elsewhere (uneditable shapes fail closed), and the edited workspace is
   then proven with real Cargo through the offline, no-deps `cargo metadata`.
   `za` never writes a `Cargo.lock`: the validation must not create one, and
   `go.work.sum` is only staged when the Go toolchain created it.
4. Creates the private context `.agents/context/<project-id>/` from the
   workspace's own templates in `.agents/context/templates/project/`: the
   project guide `AGENTS.md`, `sot.md`, `status.md`, `tools.md`, the empty
   decision register `adr.md`, an empty `decisions/`, and the execution log
   `status.log` seeded with the registration entry. Only known facts
   are filled in: the project ID and paths, the detected language, the
   module or crate identity, the workspace membership (`go.work` or root
   `Cargo.toml` members), the bootstrap phase, the date the pin was observed
   and the exact gitlink SHA as `Pinned gitlink` in `status.md`.
   Registering does **not** mark anything verified: `status.md` records
   `Verification summary: pending (no scope verified; registration observed
   the pin)`. Verification is scoped: each verified scope records the
   reviewed revision, reviewer, date and evidence in the verification
   record, the summary is derived from those rows, and a verified scope
   supports work while unrelated scopes stay pending. Everything else stays
   a `<discover: ...>` or `<decide: ...>` placeholder; no decisions or
   authorities are invented.
5. Adds the project route to the region between `<!-- za:projects:start -->`
   and `<!-- za:projects:end -->` in `AGENTS.md`, sorted by project ID. The
   rest of the file is not touched. Missing, duplicated or hand-edited markers
   make the command fail.
6. Stages exactly these changes (`.gitmodules`, the gitlink, `AGENTS.md`, the
   new context, plus `go.work` and `go.work.sum` for Go or the root
   `Cargo.toml` for Rust) and **never commits**. Review them with
   `git diff --cached`.

`za project add` **never writes to the project repository**. OpenSpec is
optional: if `<project-id>/openspec/` exists, the private context records it
as a *detected* requirement authority to confirm, not as accepted fact; if
not, the requirement authority is recorded as `unresolved`. Either way the
command never creates `openspec/` or a private substitute specification.

`za.toml` is not modified. Project membership lives in `.gitmodules`, the
language workspace (`go.work` or `Cargo.toml`), `.agents/context/<project-id>/`
and the `AGENTS.md` routing region.

Only Git contacts the repository, using your normal Git configuration and
credentials.

### Failure and rollback

If any step fails or the command is interrupted, `za` rolls the workspace
back to the clean committed state it started from: it resets the index,
removes the submodule checkout, its `.git/modules/<project-id>` directory and
its `.git/config` section, deletes the new context, and restores `AGENTS.md`,
`go.work`, the root `Cargo.toml` and `.gitmodules` byte for byte — including
a failure after the `members` insert or during the `cargo metadata`
validation. A failed addition can be retried with the same command. If
rollback cannot complete, `za` says what is left.

Exit status follows `za init`: `0` success, `1` operational failure, `2`
invalid usage.

## `za issue`

```text
za issue add [flags] "<title>" | "<raw todo.txt line>"
za issue new [flags] "<title>" | "<raw todo.txt line>"
za issue ls [flags]
za issue show <ID>
za issue move <ID> <todo|doing|done> [-commit <sha>]
za issue prompt <ID>
```

Issues live as flat records in `.za/issues/`: one Markdown file
`I-NNNNNN.md` per issue whose first line is a canonical todo.txt record and
whose body holds the lightweight XP sections (grammar and template:
`.za/templates/issue.md`). The state lives on the first line
(`@todo`, `@doing`, `@done`); listing, board rendering and state moves read
and rewrite only that line, and the Markdown body is never rewrapped. Line
2 must stay blank.

- `add` allocates the next sequential ID (`I-000001` when the directory
  holds no records yet, otherwise the highest existing number plus one,
  failing closed at `I-999999`), writes the record with the built-in
  skeleton body, and prints `<ID> .za/issues/I-NNNNNN.md`. Flags:
  `-p <A-Z>` (default `B`), `-project <id>` (repeatable or
  comma-separated; default `+workspace`; any other project must be
  registered in `.gitmodules` or exist as a top-level directory of the
  workspace), `-status <todo|doing>` (default `todo`), `-spec <wiki-path>`,
  `-adr <D-NNNN>`.
- `new` is identical except that the initial body comes from the
  workspace's own `.za/templates/issue.md` when present (its first line,
  the grammar placeholder, is dropped), falling back to the built-in
  skeleton.
- If the argument parses as a raw todo.txt line (priority, date, title,
  `+project` and `@status` tags, without `id:`), it is used as the record
  line and only the ID is allocated. A raw line cannot be combined with
  flags.
- `ls` prints tab-separated single lines
  `<ID>  <Status>  <Priority>  <Created>  <Projects>  <Title>` sorted by
  priority (`A`..`Z`, unprioritized last), then status (`@doing` before
  `@todo` before `@done`), then ID. Filters: `-status
  <active|todo|doing|done|all>` (default `active`), `-project <id>`,
  `-priority <A-Z>`. Only the first lines of the records are read.
- `show` prints the full record; the ID accepts `I-NNNNNN` or a numeric
  shorthand (`4`, `000004`).
- `move` rewrites the first line atomically (same-directory rename) while
  preserving the body byte for byte. Moving to `done` sets the completion
  date to today (UTC) and records `-commit` when given; reopening strips
  the completion prefix and the landing commit. The rewritten line is
  serialized canonically, so its tag order may change.
- `prompt` prints a deterministic, self-contained Markdown prompt for an
  AI coding agent: the task header from the record line, the required
  context routing (workspace guides, the named projects' private context,
  the `spec:` wiki pages and the `adr:` decision records), the execution
  boundary per policy P-15 (task worktree `.worktrees/<project>/<ID>/`,
  `GOWORK=off` for root-module Go projects), and the verbatim issue body.
  A `spec:` value resolves to `.za/wiki/<value>.md` (or is used as-is when
  it already names a `.za/wiki/` path) and must exist: routing an agent to
  a missing page fails the command.

All dates are UTC. Every malformed record — a first line that violates the
grammar, an `id:` tag that does not match the file name, a non-blank line
2 — fails the command loudly instead of being skipped, and `za issue`
never touches anything outside the workspace's `.za/` directory.

Exit status: `0` success, `1` operational failure (outside a workspace,
missing `.za/issues/`, malformed or missing record, exhausted ID space),
`2` invalid usage.

## `za board`

```text
za board [-project <id>]
```

Renders the plain-text Kanban view of `.za/issues/`: three sections
(`DOING`, `TODO`, `DONE`), each row showing ID, priority, `+projects` and
title, sorted by priority and ID. The output carries no ANSI escapes and is
deterministic byte for byte.

## `za wiki`

```text
za wiki new [-title "<Title>"] <slug>
za wiki ls
za wiki show <slug>
```

Wiki pages are the workspace's private specification and knowledge base in
`.za/wiki/`: flat, organically named Markdown files (navigation:
`.za/wiki/index.md`).

- `new` creates `.za/wiki/<slug>.md` exclusively (an existing page or
  directory is never overwritten) and prints the created path. The body
  comes from the workspace's own `.za/templates/wiki.md` when present (its
  `# ` heading is replaced with the title), falling back to a clean draft
  skeleton; with `-title` omitted, the title is derived from the slug
  (`go-minolas-architecture` becomes `Go Minolas Architecture`).
- `ls` prints deterministic tab-separated lines
  `<slug>  <path>  <title>` sorted lexicographically by slug. Only regular
  `.md` files directly in `.za/wiki/` are listed (subdirectories such as
  `img/` are skipped); titles are each page's first `# ` heading, read
  through a bounded stream, with the slug as fallback.
- `show` prints the full page; the slug accepts the name with or without
  the `.md` suffix.

Slugs are validated fail-closed: empty, dot-prefixed, separator-bearing or
traversal names, control characters and the reserved `img/` asset
directory are rejected, and content with CR line endings is an error. Exit
status follows the other commands: `0` success, `1` operational failure,
`2` invalid usage.

## `za version`

```text
za version
```

Prints the version of the executable: one summary line and one detail line
per available fact.

```text
za (devel) (development build)
  module:    github.com/tnotstar/go-za
  revision:  3996d08e0e873ecd3fcd38a755f7c9fc2a666d36 (modified)
  committed: 2026-11-04T11:26:48Z
  go:        go1.27.2
```

### Version kinds

The version kind distinguishes how the executable was produced:

| Kind | Main module version | Typical producer |
|---|---|---|
| `release` | Tagged semantic version (`v1.2.3`, `v1.2.3-rc.1+build.5`) | `go install github.com/tnotstar/go-za/cmd/za@v1.2.3` |
| `Go pseudo-version` | Pseudo-version (`v0.0.0-20261104112648-3996d08e0e87`) | `go install ...@<branch-or-commit>` without a tag |
| `development build` | `(devel)` | `go build ./cmd/za`, `make build`, `make install` |

### Metadata source and precedence

The version is read exclusively from the **Go runtime build information**
embedded at link time (`runtime/debug.ReadBuildInfo`): the main module
version and the `vcs.revision`, `vcs.time` and `vcs.modified` settings.

- There is deliberately **no build-time injection** (`-ldflags -X ...`) and
  **no version constant in the source**: one mechanism, one truth, no drift.
  Every supported build path (`go build`, `make build`, `make install`,
  `go install ...@version`) stamps the same metadata automatically.
- Classification precedence: a build whose source had **uncommitted
  modifications** (`vcs.modified=true`) is a **development build**, however
  the toolchain named the version — current Go stamps local builds as a
  pseudo-version of HEAD with `+dirty` rather than the literal `(devel)`.
  The version string alone never decides the kind: a pseudo-version
  carrying `+dirty` without `vcs.modified=true` stays a `Go pseudo-version`.
  Otherwise `(devel)` is a development build, a pseudo-version shape is a
  `Go pseudo-version`, and a tagged semantic version is a `release`. A local
  untagged build therefore stays identifiable as a development build while
  its source is modified, and a clean tree is reported as the exact
  pseudo-version of its commit.
- When VCS stamping is unavailable — a build outside a Git checkout, or with
  `-buildvcs=false` — the revision line reports it explicitly as
  unavailable. `za` never invents values.
- `committed:` is the commit time of the stamped source revision (VCS
  provenance), not the wall-clock time of the build.

### Independence and compatibility

`za version` performs no workspace discovery and invokes no external tool:
it works outside any `za` workspace, without Git and without network
access. Existing commands and the `--help` output are unchanged except for
the added `version` line.

## Generated workspace

```text
<workspace>/
├── .git/                    git init
├── za.toml                  za workspace manifest (schema = 4)
├── .gitignore               ignores .worktrees/, .agents/runtime/, .venv/
├── .gitattributes           LF line endings
├── .editorconfig
├── AGENTS.md                root agent guide, with a za-managed project region
├── CLAUDE.md                points Claude Code to AGENTS.md
├── go.work                  --go only, written by the Go toolchain
├── pyproject.toml           --python only, written by uv, then extended
├── Cargo.toml               --rust only: virtual workspace, no members
├── .agents/
│   ├── context/
│   │   ├── AGENTS.md        common guide (routes to mandatory policy and topics)
│   │   ├── policy.md        COMMON policy: MUST rules for the whole workspace
│   │   ├── tooling.md       COMMON tooling: integration constraints per capability
│   │   ├── templates/project/   PROJECT context templates, including status.log
│   │   └── <project-id>/    PROJECT contexts (created by za project add)
│   ├── hooks/{meta,public}/ reserved for future hook management
│   └── runtime/             local, ignored
├── .za/                     workspace issues, wiki and templates (XP workflow)
│   ├── issues/              flat I-NNNNNN.md issues (canonical todo.txt first line)
│   ├── wiki/                private specification and knowledge pages
│   │   ├── index.md         wiki navigation root
│   │   └── img/             optional assets
│   └── templates/           canonical issue and wiki page templates
└── .worktrees/              local, ignored
```

The context has two levels in the workspace, both under `.agents/context/`:
COMMON context (the direct files, owned by the workspace maintainers) and
PROJECT context under `<project-id>/`. GLOBAL preferences are a third,
optional level **outside** the workspace: they belong to the user's coding
tool, and `za` never discovers, provisions or modifies them, never requires
them (initialization works with an empty user profile), and never copies
personal preferences into a project.

Three guides serve the routing decisions: the root `AGENTS.md` (which
repository and task scope?), the common guide `.agents/context/AGENTS.md`
(which shared rules apply?), and each project's
`.agents/context/<id>/AGENTS.md` (what authority, commands or decision
governs this change?). The common guide routes to the mandatory
`policy.md` and to `tooling.md`, which documents integration behavior only
for the languages you select. It never duplicates the project list (that is
derived from `.gitmodules`) and never embeds issue bodies. Leaf collections
such as issues and decisions are reached by ID or reference. The `.za/`
layer at the workspace root holds the workspace's issues (`.za/issues/`,
flat `I-NNNNNN.md` files whose first line is a canonical todo.txt record),
its private specification wiki (`.za/wiki/`) and the canonical templates
for both (`.za/templates/`). Project IDs must not collide with context
infrastructure names; `za project add` rejects reserved names and
case-equivalent collisions, including hidden dot-prefixed names such as
`.za` or `.agents`.

> **`.worktrees/` is ignored for hygiene, not confidentiality.** Private
> context stays private because it lives in the private meta-repository,
> outside every public project's Git working tree. The workspace stays
> correct even if the ignore rules are removed.

### Workspace manifest

Every initialized workspace has a `za.toml` at its root. `za` writes it, and it
is tracked in the meta-repository:

```toml
schema = 4

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

`za` always writes all three capability booleans. Reading an otherwise
valid schema-4 manifest without `workspace.rust` means `rust = false`;
invalid types are rejected.

The manifest belongs to `za`, not to the agent context under `.agents/`. It
only marks the directory as a `za` workspace. Projects, paths and tool
versions are deliberately left out, because they have their own sources of
truth.

In `AGENTS.md`, the region between `<!-- za:projects:start -->` and
`<!-- za:projects:end -->` is maintained by `za project add`. Keep your own
edits outside it.

### Schemas and compatibility

Schema 4 is the guide-based context layout: the root `AGENTS.md`, the common
guide `.agents/context/AGENTS.md` and per-project
`.agents/context/<id>/AGENTS.md` guides route to COMMON policy, project
authorities (`sot.md`) and the decision register; GLOBAL is an optional,
user-managed level outside the workspace. Earlier schemas described
different layouts:

| Schema | Layout |
|---|---|
| 1 | Nested `.agents/context/_global/` and `.agents/context/projects/` (previous releases of `za`) |
| 2 | Flat layout with mandatory OpenSpec assumptions (a designed intermediate, never shipped) |
| 3 | Flat layout with a context-root `INDEX.md` router, GLOBAL context files, generated language standards and `source-of-truth.md` |

**There is no migration.** This version of `za` writes schema-4 workspaces
only. Every command that reads `za.toml` rejects unsupported schemas,
including schema 3, before mutating anything, and never flattens or
rewrites an old workspace as a side effect. Schema-3 workspaces keep
working with the `za` releases that match their schema.

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
`sot.md` or the project guide `AGENTS.md`).

`za` binaries and manifests are matched strictly in both directions: this
version rejects schema-3 workspaces, and older `za` binaries reject
schema-4 manifests as unsupported instead of partially handling them.
Binaries from before the Rust capability reject a
Rust-enabled manifest outright with `unknown key workspace.rust` — they never
silently ignore the capability or partially apply it. A Rust-enabled
workspace therefore requires a `za` binary with Rust support; there is no
downgrade path, and no schema bump was needed for this additive capability.

## Development

```bash
make check            # gofmt check, go vet, unit tests, build
make integration      # run against the real git, go and uv
make integration-cargo # also run the real-cargo Rust tests (cargo/rustc >= 1.85.0)
make golden           # regenerate scaffold golden files after template edits
make help             # list every target
```

The Makefile needs GNU Make and a POSIX shell; on Windows, use the shell
shipped with Git for Windows. Every target wraps a plain `go` command, so you
can also run those directly.

The default test suite needs neither `uv`, `cargo` nor network access. Most
tests use a fake command runner; the `za project add` tests also run the real
`git` and `go` against local repositories with an isolated Git configuration,
and the Rust onboarding tests fake Cargo deterministically. `go test -short`
skips the real-tool tests.

### Dependency policy

The default is **zero external Go dependencies**. Nominal exceptions require
an accepted decision record in the workspace context and are limited to
implementations of formal specifications the Go standard library does not
cover, with zero transitive dependencies in `go.sum`. The current allowlist:
`github.com/BurntSushi/toml` (TOML v1.0.0) for parsing `Cargo.toml`
manifests; the workspace's own `za.toml` keeps its strict hand-rolled subset
parser, which encodes schema 4's fail-closed acceptance contract.

The repository forces LF line endings through `.gitattributes`. Templates are
embedded byte-for-byte and golden tests compare exact bytes, so do not
override this with `core.autocrlf` settings.

Every embedded template carries the uniform `.tmpl` suffix, including the
root guide `AGENTS.md.tmpl` (rendered to `AGENTS.md`); `internal/tmpl` owns
that convention: a file without the suffix is an error, and a template
referencing a field the caller does not provide fails the render.

## License

[BSD 3-Clause](LICENSE).
