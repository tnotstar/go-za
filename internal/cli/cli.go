// Package cli dispatches za subcommands and maps their outcomes to exit codes.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/tnotstar/go-za/internal/initcmd"
	"github.com/tnotstar/go-za/internal/project"
	"github.com/tnotstar/go-za/internal/tool"
	"github.com/tnotstar/go-za/internal/version"
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
  init     Initialize an empty directory as a za meta-workspace
  project  Manage the projects of a za workspace
  version  Print the version of the za executable

Run "za <command> --help" for details about a command.
`

// versionUsage describes the version subcommand, mirroring the style of the
// other subcommand usage texts.
const versionUsage = `za version

Print the version of the za executable: its module version (tagged
release, Go pseudo-version or local development build) and the VCS
revision, commit time and modified-source state stamped at link time.
It reads only the executable's own Go build information and never
touches the filesystem, Git or the network; facts that were not stamped
are reported as unavailable.

It takes no arguments.
`

// Env holds the process-level dependencies of every subcommand.
type Env struct {
	Runner tool.Runner
	Getwd  func() (string, error)
	Now    func() time.Time
}

// Run executes the za command line args (without the program name) and
// returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "init":
		return runInit(ctx, args[1:], stdout, stderr, initcmd.Env{Runner: env.Runner, Getwd: env.Getwd})
	case "project":
		return runProject(ctx, args[1:], stdout, stderr, project.Env{Runner: env.Runner, Getwd: env.Getwd, Now: env.Now})
	case "version":
		return runVersion(args[1:], stdout, stderr)
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

func runProject(ctx context.Context, args []string, stdout, stderr io.Writer, env project.Env) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, project.Usage)
		return ExitUsage
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, project.Usage)
		return ExitOK
	case "add":
		return runProjectAdd(ctx, args[1:], stdout, stderr, env)
	default:
		fmt.Fprintf(stderr, "error: unknown project command %q\nRun \"za project --help\" for usage.\n", args[0])
		return ExitUsage
	}
}

func runProjectAdd(ctx context.Context, args []string, stdout, stderr io.Writer, env project.Env) int {
	url, err := project.ParseAddArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, project.AddUsage)
		return ExitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\nRun \"za project add --help\" for usage.\n", err)
		return ExitUsage
	}

	res, err := project.Add(ctx, url, env)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ExitFailure
	}
	fmt.Fprint(stdout, res.Summary())
	return ExitOK
}

// runVersion prints the executable's build identity. It uses no process
// dependencies: the version comes from the runtime build information, so it
// works outside any workspace, without Git and without the network.
func runVersion(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
	case len(args) == 1:
		switch args[0] {
		case "-h", "-help", "--help", "help":
			fmt.Fprint(stdout, versionUsage)
			return ExitOK
		default:
			fmt.Fprintf(stderr, "error: unexpected argument %q\nRun \"za version --help\" for usage.\n", args[0])
			return ExitUsage
		}
	default:
		fmt.Fprintf(stderr, "error: too many arguments\nRun \"za version --help\" for usage.\n")
		return ExitUsage
	}
	if err := version.Inspect(version.Read()).Render(stdout); err != nil {
		fmt.Fprintf(stderr, "error: print version: %v\n", err)
		return ExitFailure
	}
	return ExitOK
}
