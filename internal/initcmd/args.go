// Package initcmd implements "za init".
package initcmd

import (
	"errors"
	"strings"

	"github.com/tnotstar/go-minolas/pkg/cli/argparse"
)

// Usage is the help text for "za init".
const Usage = `Usage:
  za init [--go] [--python] [--rust] [PATH]

Initialize PATH as a private za meta-workspace. PATH defaults to the current
directory. It must be an empty directory (hidden entries count) or must not
exist yet; missing directories are created.

At least one workspace type is required:
  --go       initialize a Go workspace with "go work init"
  --python   initialize a uv integration workspace with "uv init"
  --rust     initialize a root Cargo workspace scaffold (virtual manifest,
             no members)

Required executables: git always; go with --go; uv with --python; cargo and
rustc (version 1.85.0 or newer) with --rust. za never installs or downloads
toolchains.
`

// Options are the parsed "za init" arguments.
type Options struct {
	Go     bool
	Python bool
	Rust   bool
	// Path is the target directory; empty means the current directory.
	Path string
}

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// ParseArgs parses "za init" arguments. Flags may appear before or after the
// path; everything after "--" is positional. It returns argparse.ErrHelp when
// help was requested and a *UsageError for invalid usage.
func ParseArgs(args []string) (Options, error) {
	p := argparse.NewArgumentParser("init", "")
	goFlag := p.Bool("--go")
	pythonFlag := p.Bool("--python")
	rustFlag := p.Bool("--rust")
	pathArg := p.String("PATH")

	if err := p.Parse(args); err != nil {
		if errors.Is(err, argparse.ErrHelp) {
			return Options{}, err
		}
		if strings.Contains(err.Error(), "unrecognized positional argument") {
			return Options{}, &UsageError{"too many arguments: expected at most one PATH"}
		}
		return Options{}, &UsageError{err.Error()}
	}

	opts := Options{
		Go:     *goFlag,
		Python: *pythonFlag,
		Rust:   *rustFlag,
	}
	switch {
	case p.Seen("PATH") && *pathArg == "":
		return Options{}, &UsageError{"PATH must not be empty"}
	case !opts.Go && !opts.Python && !opts.Rust:
		return Options{}, &UsageError{"at least one workspace type is required: --go, --python and/or --rust"}
	}
	if p.Seen("PATH") {
		opts.Path = *pathArg
	}
	return opts, nil
}
