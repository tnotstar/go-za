// Package cli dispatches za subcommands and maps their outcomes to exit codes.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tnotstar/go-za/internal/initcmd"
)

// Exit codes returned by Run.
const (
	ExitOK      = 0 // success
	ExitFailure = 1 // operational or runtime failure
	ExitUsage   = 2 // invalid command-line usage
)

const usage = `za manages private multi-repository AI/development workspaces.

Usage:
  za <command> [arguments]

Commands:
  init    Initialize an empty directory as a za meta-workspace

Run "za <command> --help" for details about a command.
`

// Run executes the za command line args (without the program name) and
// returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, env initcmd.Env) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "init":
		return runInit(ctx, args[1:], stdout, stderr, env)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\nRun \"za --help\" for usage.\n", args[0])
		return ExitUsage
	}
}

func runInit(ctx context.Context, args []string, stdout, stderr io.Writer, env initcmd.Env) int {
	opts, err := initcmd.ParseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, initcmd.Usage)
		return ExitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\nRun \"za init --help\" for usage.\n", err)
		return ExitUsage
	}

	res, err := initcmd.Run(ctx, opts, env)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ExitFailure
	}
	fmt.Fprint(stdout, res.Summary())
	return ExitOK
}
