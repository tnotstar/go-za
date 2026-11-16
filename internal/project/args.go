// Package project implements "za project" subcommands.
package project

import (
	"errors"
	"strings"

	"github.com/tnotstar/go-minolas/pkg/cli/argparse"
)

// Usage is the help text for "za project".
const Usage = `Usage:
  za project <command> [arguments]

Commands:
  add     Add a Go or Rust repository as a workspace project

Run "za project <command> --help" for details about a command.
`

// AddUsage is the help text for "za project add".
const AddUsage = `Usage:
  za project add <repo-url>

Add the repository at <repo-url> to the enclosing za workspace as the
top-level Git submodule <project-id>/, where <project-id> is the last
component of <repo-url> without a trailing ".git".

The command detects the project language after cloning:
  - a root go.mod makes a Go project: it is added to the root go.work with
    "go work use";
  - a root Cargo.toml makes a Rust project: it must be a single root crate
    ([package] without [workspace], no nested Cargo.toml), and it is added
    to the members of the root Cargo.toml and verified offline with
    "cargo metadata --no-deps --offline";
  - both or neither at the repository root are rejected.

For every project it:
  - runs "git submodule add -- <repo-url> <project-id>";
  - creates the private context .agents/context/<project-id>/;
  - adds the project route to the za-managed region of AGENTS.md;
  - stages the whole change without committing it.

It never writes to the project repository. If any step fails, the workspace
is rolled back to its previous state.

Requirements: a schema-4 Go- and/or Rust-enabled za workspace (za.toml),
with a committed bootstrap and no uncommitted changes; executables git and
go for Go projects, cargo and rustc (1.85.0 or newer) for Rust projects.
`

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// ParseAddArgs parses "za project add" arguments and returns the repository
// URL. It returns argparse.ErrHelp when help was requested and a *UsageError for
// invalid usage.
func ParseAddArgs(args []string) (string, error) {
	p := argparse.NewArgumentParser("project add", "")
	repoURL := p.String("repo-url")
	if err := p.Parse(args); err != nil {
		if errors.Is(err, argparse.ErrHelp) {
			return "", err
		}
		if strings.Contains(err.Error(), "unrecognized positional argument") {
			return "", &UsageError{"too many arguments: expected exactly one repository URL"}
		}
		return "", &UsageError{err.Error()}
	}
	switch {
	case !p.Seen("repo-url"):
		return "", &UsageError{"missing repository URL"}
	case *repoURL == "":
		return "", &UsageError{"repository URL must not be empty"}
	default:
		return *repoURL, nil
	}
}
