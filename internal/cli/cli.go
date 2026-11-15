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
	"github.com/tnotstar/go-za/internal/issue"
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
  issue    Manage the workspace issues (.za/issues/)
  board    Show the Kanban board of the workspace issues
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
	case "issue":
		return runIssue(ctx, args[1:], stdout, stderr, issue.Env{Getwd: env.Getwd, Now: env.Now})
	case "board":
		return runBoard(ctx, args[1:], stdout, stderr, issue.Env{Getwd: env.Getwd, Now: env.Now})
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

// runIssue dispatches the "za issue" subcommands. The commands perform pure
// file operations on the workspace's .za/issues/ records, so they need no
// native command runner and no context.
func runIssue(_ context.Context, args []string, stdout, stderr io.Writer, env issue.Env) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, issue.Usage)
		return ExitUsage
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, issue.Usage)
		return ExitOK
	case "add":
		return runIssueAdd(args[1:], stdout, stderr, env, issue.DefaultBody, issue.AddUsage)
	case "new":
		return runIssueNew(args[1:], stdout, stderr, env)
	case "ls":
		return runIssueLs(args[1:], stdout, stderr, env)
	case "show":
		return runIssueShow(args[1:], stdout, stderr, env)
	case "move":
		return runIssueMove(args[1:], stdout, stderr, env)
	default:
		fmt.Fprintf(stderr, "error: unknown issue command %q\nRun \"za issue --help\" for usage.\n", args[0])
		return ExitUsage
	}
}

// runIssueAdd implements "za issue add": the fast path with the built-in
// skeleton body.
func runIssueAdd(args []string, stdout, stderr io.Writer, env issue.Env, body, usage string) int {
	req, err := issue.ParseAddArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "issue")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	created, err := issue.Add(root, req, body, env.Now())
	if err != nil {
		return failureExit(stderr, err)
	}
	fmt.Fprintf(stdout, "%s %s\n", created.ID, issue.RecordPath(created.ID))
	return ExitOK
}

// runIssueNew implements "za issue new": the template-backed initializer.
func runIssueNew(args []string, stdout, stderr io.Writer, env issue.Env) int {
	req, err := issue.ParseNewArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, issue.AddUsage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "issue")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	body, err := issue.TemplateBody(root)
	if err != nil {
		return failureExit(stderr, err)
	}
	created, err := issue.Add(root, req, body, env.Now())
	if err != nil {
		return failureExit(stderr, err)
	}
	fmt.Fprintf(stdout, "%s %s\n", created.ID, issue.RecordPath(created.ID))
	return ExitOK
}

// runIssueLs implements "za issue ls".
func runIssueLs(args []string, stdout, stderr io.Writer, env issue.Env) int {
	opts, err := issue.ParseLsArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, issue.LsUsage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "issue")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	issues, err := issue.ScanSummaries(issue.IssuesDir(root))
	if err != nil {
		return failureExit(stderr, err)
	}
	issues = issue.Filter(issues, opts.Status, opts.Project, opts.Priority)
	issue.SortByPriorityStatusID(issues)
	fmt.Fprint(stdout, issue.RenderList(issues))
	return ExitOK
}

// runIssueShow implements "za issue show".
func runIssueShow(args []string, stdout, stderr io.Writer, env issue.Env) int {
	opts, err := issue.ParseShowArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, issue.ShowUsage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "issue")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	data, err := issue.ReadRecord(issue.IssuesDir(root), opts.ID)
	if err != nil {
		return failureExit(stderr, err)
	}
	if _, werr := stdout.Write(data); werr != nil {
		return failureExit(stderr, werr)
	}
	return ExitOK
}

// runIssueMove implements "za issue move" and prints the rewritten first
// line.
func runIssueMove(args []string, stdout, stderr io.Writer, env issue.Env) int {
	opts, err := issue.ParseMoveArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, issue.MoveUsage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "issue")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	iss, err := issue.Move(issue.IssuesDir(root), opts.ID, opts.To, opts.Commit, env.Now())
	if err != nil {
		return failureExit(stderr, err)
	}
	fmt.Fprintln(stdout, issue.Serialize(iss))
	return ExitOK
}

// runBoard implements "za board".
func runBoard(_ context.Context, args []string, stdout, stderr io.Writer, env issue.Env) int {
	opts, err := issue.ParseBoardArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, issue.BoardUsage)
		return ExitOK
	}
	if err != nil {
		return usageExit(stderr, err, "board")
	}
	root, err := env.Root()
	if err != nil {
		return failureExit(stderr, err)
	}
	issues, err := issue.ScanSummaries(issue.IssuesDir(root))
	if err != nil {
		return failureExit(stderr, err)
	}
	fmt.Fprint(stdout, issue.Board(issue.Filter(issues, "all", opts.Project, 0)))
	return ExitOK
}

func usageExit(stderr io.Writer, err error, command string) int {
	fmt.Fprintf(stderr, "error: %v\nRun \"za %s --help\" for usage.\n", err, command)
	return ExitUsage
}

func failureExit(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "error: %v\n", err)
	return ExitFailure
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
