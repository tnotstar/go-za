// Package project implements "za project" subcommands.
package project

import (
	"errors"
	"flag"
	"io"
)

// Usage is the help text for "za project".
const Usage = `Usage:
  za project <command> [arguments]

Commands:
  add     Add a Go repository as a workspace project

Run "za project <command> --help" for details about a command.
`

// AddUsage is the help text for "za project add".
const AddUsage = `Usage:
  za project add <repo-url>

Add the repository at <repo-url> to the enclosing za workspace as the
top-level Git submodule <project-id>/, where <project-id> is the last
component of <repo-url> without a trailing ".git".

The command:
  - runs "git submodule add -- <repo-url> <project-id>";
  - requires <project-id>/go.mod and runs "go work use ./<project-id>";
  - creates the private context .agents/context/<project-id>/;
  - adds the project route to the za-managed region of AGENTS.md;
  - stages the whole change without committing it.

It never writes to the project repository. If any step fails, the workspace
is rolled back to its previous state.

Requirements: a schema-4 Go-enabled za workspace (za.toml), with a committed
bootstrap and no uncommitted changes; executables git and go.
`

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// ParseAddArgs parses "za project add" arguments and returns the repository
// URL. It returns flag.ErrHelp when help was requested and a *UsageError for
// invalid usage.
func ParseAddArgs(args []string) (string, error) {
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", err
		}
		return "", &UsageError{err.Error()}
	}
	switch rest := fs.Args(); {
	case len(rest) == 0:
		return "", &UsageError{"missing repository URL"}
	case len(rest) > 1:
		return "", &UsageError{"too many arguments: expected exactly one repository URL"}
	case rest[0] == "":
		return "", &UsageError{"repository URL must not be empty"}
	default:
		return rest[0], nil
	}
}
