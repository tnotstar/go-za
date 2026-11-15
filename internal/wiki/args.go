package wiki

import (
	"errors"
	"flag"
	"io"
)

// Usage is the help text for "za wiki".
const Usage = `Usage:
  za wiki <command> [arguments]

Manage the workspace wiki pages in .za/wiki/: flat, organically named
Markdown files — the private source of truth for observable behavior,
architecture and domain knowledge (navigation: .za/wiki/index.md).

Commands:
  new   Create a page from the workspace wiki template or the draft skeleton
  ls    List pages sorted by slug with their first heading as title
  show  Print the full content of one page

Run "za wiki <command> --help" for details about a command.
`

// NewUsage is the help text for "za wiki new".
const NewUsage = `Usage:
  za wiki new [-title "<Title>"] <slug>

Create .za/wiki/<slug>.md exclusively (an existing page or directory is
never overwritten) and print the created workspace-relative path.

The body comes from the workspace's own .za/templates/wiki.md when present
(its "# " heading is replaced with the title), falling back to a clean
draft skeleton. With -title omitted, a default title is derived from the
slug (go-minolas-architecture becomes "Go Minolas Architecture").

The slug accepts the name with or without the .md suffix and rejects
empty, dot-prefixed, separator-bearing or traversal names and the reserved
img/ asset directory, fail-closed.
`

// LsUsage is the help text for "za wiki ls".
const LsUsage = `Usage:
  za wiki ls

List the wiki pages as deterministic tab-separated lines

  <slug>  <path>  <title>

sorted lexicographically by slug. Only regular .md files directly in
.za/wiki/ are listed (subdirectories such as img/ are skipped). Titles are
each page's first "# " heading, read through a bounded stream; pages
without a heading fall back to their slug.
`

// ShowUsage is the help text for "za wiki show".
const ShowUsage = `Usage:
  za wiki show <slug>

Print the full content of .za/wiki/<slug>.md. The slug accepts the name
with or without the .md suffix.
`

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// NewArgs is the parsed "za wiki new" invocation.
type NewArgs struct {
	Slug  string
	Title string
}

// ParseNewArgs parses "za wiki new" arguments.
func ParseNewArgs(args []string) (NewArgs, error) {
	fs := flag.NewFlagSet("wiki new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	title := fs.String("title", "", "page title (default: derived from the slug)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return NewArgs{}, err
		}
		return NewArgs{}, &UsageError{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return NewArgs{}, &UsageError{"expected exactly one page slug"}
	}
	var req NewArgs
	req.Slug, req.Title = rest[0], *title
	return req, nil
}

// ParseShowArgs parses "za wiki show" arguments.
func ParseShowArgs(args []string) (string, error) {
	fs := flag.NewFlagSet("wiki show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", err
		}
		return "", &UsageError{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return "", &UsageError{"expected exactly one page slug"}
	}
	slug, err := NormalizeSlug(rest[0])
	if err != nil {
		return "", &UsageError{err.Error()}
	}
	return slug, nil
}
