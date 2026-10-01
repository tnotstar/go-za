// Package initcmd implements "za init".
package initcmd

import (
	"errors"
	"flag"
	"io"
)

// Usage is the help text for "za init".
const Usage = `Usage:
  za init [--go] [--python] [PATH]

Initialize PATH as a private za meta-workspace. PATH defaults to the current
directory. It must be an empty directory (hidden entries count) or must not
exist yet; missing directories are created.

At least one workspace type is required:
  --go       initialize a Go workspace with "go work init"
  --python   initialize a uv integration workspace with "uv init"

Required executables: git always; go with --go; uv with --python.
`

// Options are the parsed "za init" arguments.
type Options struct {
	Go     bool
	Python bool
	// Path is the target directory; empty means the current directory.
	Path string
}

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// ParseArgs parses "za init" arguments. Flags may appear before or after the
// path; everything after "--" is positional. It returns flag.ErrHelp when
// help was requested and a *UsageError for invalid usage.
func ParseArgs(args []string) (Options, error) {
	var opts Options
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.Go, "go", false, "")
	fs.BoolVar(&opts.Python, "python", false, "")

	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Options{}, err
			}
			return Options{}, &UsageError{err.Error()}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			positional = append(positional, rest...)
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}

	switch {
	case len(positional) > 1:
		return Options{}, &UsageError{"too many arguments: expected at most one PATH"}
	case len(positional) == 1 && positional[0] == "":
		return Options{}, &UsageError{"PATH must not be empty"}
	case !opts.Go && !opts.Python:
		return Options{}, &UsageError{"at least one workspace type is required: --go and/or --python"}
	}
	if len(positional) == 1 {
		opts.Path = positional[0]
	}
	return opts, nil
}
