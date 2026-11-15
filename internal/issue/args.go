package issue

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Usage is the help text for "za issue".
const Usage = `Usage:
  za issue <command> [arguments]

Manage the workspace issue records in .za/issues/: flat I-NNNNNN.md files
whose first line is a canonical todo.txt record (grammar:
.za/templates/issue.md).

Commands:
  add     Create an issue from a title or a raw todo.txt line (fast path)
  new     Create an issue initialized from the workspace issue template
  ls      List issues sorted by priority, status and ID
  show    Print the full content of one issue record
  move    Move an issue to another state (todo, doing, done)

Run "za issue <command> --help" for details about a command.
`

// AddUsage is the help text for "za issue add" and "za issue new".
const AddUsage = `Usage:
  za issue add  [flags] "<title>" | "<raw todo.txt line>"
  za issue new  [flags] "<title>" | "<raw todo.txt line>"

Create .za/issues/I-NNNNNN.md with the next free sequential ID, the
canonical todo.txt first line and the lightweight XP body sections, and
print "<ID> .za/issues/I-NNNNNN.md".

"add" writes the built-in skeleton body. "new" uses the body shape of
.za/templates/issue.md when the workspace carries one (its first line, the
grammar placeholder, is dropped), falling back to the built-in skeleton.

If the argument parses as a raw todo.txt line (priority, date, title,
+project and @status tags, without id:), it is used as the record line and
the ID is allocated; raw lines cannot be combined with flags. Otherwise the
argument is the title and the flags shape the record:

  -p <A-Z>            priority letter (default B)
  -project <id>       affected project; repeatable or comma-separated
                      (default +workspace; other projects must be registered
                      in .gitmodules or exist as top-level directories)
  -status <todo|doing> initial state (default todo)
  -spec <wiki-path>   link to the governing .za/wiki/ page
  -adr <D-NNNN>       link to a governing decision record

The creation date is today (UTC). New issues never start as @done.
`

// LsUsage is the help text for "za issue ls".
const LsUsage = `Usage:
  za issue ls [flags]

List issue records as tab-separated single lines:

  <ID>  <Status>  <Priority>  <Created>  <Projects>  <Title>

sorted by priority (A..Z, unprioritized last), then status (@doing before
@todo before @done), then ID. Only the first lines of the records are read.

  -status <active|todo|doing|done|all>  filter (default active: @todo + @doing)
  -project <id>                         only issues naming +<id>
  -priority <A-Z>                       only issues with that priority
`

// ShowUsage is the help text for "za issue show".
const ShowUsage = `Usage:
  za issue show <ID>

Print the full content of .za/issues/<ID>.md. The ID accepts I-NNNNNN or a
numeric shorthand (4, 000004).
`

// MoveUsage is the help text for "za issue move".
const MoveUsage = `Usage:
  za issue move <ID> <todo|doing|done> [-commit <sha>]

Rewrite the todo.txt first line of the record to the new state while
preserving the Markdown body (line 2 onwards) byte for byte. Moving to done
sets the completion date to today (UTC) and records -commit when given;
reopening strips the completion prefix and the landing commit. Prints the
rewritten first line.

The ID accepts I-NNNNNN or a numeric shorthand (4, 000004).
`

// BoardUsage is the help text for "za board".
const BoardUsage = `Usage:
  za board [-project <id>]

Render the plain-text Kanban view of .za/issues/: one section per state
(DOING, TODO, DONE), each row showing ID, priority, +projects and title,
sorted by priority and ID. Only the first lines of the records are read.

  -project <id>  only issues naming +<id>
`

// UsageError reports invalid command-line usage.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

var adrPattern = regexp.MustCompile(`^D-\d{4}$`)

// projectList accumulates repeated or comma-separated -project values.
type projectList []string

func (p *projectList) String() string { return strings.Join(*p, ",") }

func (p *projectList) Set(v string) error {
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		if !validProjectName(name) {
			return fmt.Errorf("malformed project name %q", name)
		}
		*p = append(*p, name)
	}
	return nil
}

// AddArgs is the parsed "za issue add"/"za issue new" invocation.
type AddArgs struct {
	Issue Issue // ID is allocated on creation; Created is filled on creation
	Raw   bool  // the positional argument was a raw todo.txt line
}

func parseAddArgs(cmd string, args []string) (AddArgs, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	prio := fs.String("p", "B", "priority letter A-Z")
	var projects projectList
	fs.Var(&projects, "project", "affected project, repeatable or comma-separated")
	status := fs.String("status", string(Todo), "initial state: todo or doing")
	spec := fs.String("spec", "", "governing wiki page path")
	adr := fs.String("adr", "", "governing decision record ID")

	// Flags may appear before or after the positional argument; everything
	// after "--" is positional.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return AddArgs{}, err
			}
			return AddArgs{}, &UsageError{err.Error()}
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
	if len(positional) != 1 {
		return AddArgs{}, &UsageError{"expected exactly one title or raw todo.txt line"}
	}

	// A raw todo.txt line is used as-is and cannot be combined with flags.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if iss, err := ParseAllocating(positional[0]); err == nil {
		if len(setFlags) > 0 {
			return AddArgs{}, &UsageError{"a raw todo.txt line cannot be combined with flags"}
		}
		return AddArgs{Issue: iss, Raw: true}, nil
	}

	var req AddArgs
	req.Issue.Title = positional[0]
	if len(*prio) != 1 || (*prio)[0] < 'A' || (*prio)[0] > 'Z' {
		return req, &UsageError{fmt.Sprintf("invalid priority %q (want a single letter A-Z)", *prio)}
	}
	req.Issue.Priority = (*prio)[0]
	if *status != string(Todo) && *status != string(Doing) {
		return req, &UsageError{fmt.Sprintf("invalid status %q (want todo or doing)", *status)}
	}
	req.Issue.Status = Status(*status)
	req.Issue.Projects = projects
	if len(req.Issue.Projects) == 0 {
		req.Issue.Projects = []string{"workspace"}
	}
	if *spec != "" {
		req.Issue.Spec = *spec
	}
	if *adr != "" {
		if !adrPattern.MatchString(*adr) {
			return req, &UsageError{fmt.Sprintf("invalid decision ID %q (want D-NNNN)", *adr)}
		}
		req.Issue.ADR = *adr
	}
	return req, nil
}

// ParseAddArgs parses "za issue add" arguments.
func ParseAddArgs(args []string) (AddArgs, error) { return parseAddArgs("issue add", args) }

// ParseNewArgs parses "za issue new" arguments; the flag set is identical.
func ParseNewArgs(args []string) (AddArgs, error) { return parseAddArgs("issue new", args) }

// LsArgs is the parsed "za issue ls" invocation.
type LsArgs struct {
	Status   string // active (default), todo, doing, done or all
	Project  string
	Priority byte
}

// ParseLsArgs parses "za issue ls" arguments.
func ParseLsArgs(args []string) (LsArgs, error) {
	fs := flag.NewFlagSet("issue ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "active", "filter: active, todo, doing, done or all")
	project := fs.String("project", "", "only issues naming +<id>")
	prio := fs.String("priority", "", "only issues with this priority letter")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return LsArgs{}, err
		}
		return LsArgs{}, &UsageError{err.Error()}
	}
	switch *status {
	case "active", "todo", "doing", "done", "all":
	default:
		return LsArgs{}, &UsageError{fmt.Sprintf("invalid status filter %q (want active, todo, doing, done or all)", *status)}
	}
	var req LsArgs
	req.Status, req.Project = *status, *project
	if *prio != "" {
		if len(*prio) != 1 || (*prio)[0] < 'A' || (*prio)[0] > 'Z' {
			return req, &UsageError{fmt.Sprintf("invalid priority %q (want a single letter A-Z)", *prio)}
		}
		req.Priority = (*prio)[0]
	}
	if rest := fs.Args(); len(rest) > 0 {
		return req, &UsageError{"unexpected arguments after the flags"}
	}
	return req, nil
}

// ShowArgs is the parsed "za issue show" invocation.
type ShowArgs struct {
	ID string
}

// ParseShowArgs parses "za issue show" arguments.
func ParseShowArgs(args []string) (ShowArgs, error) {
	fs := flag.NewFlagSet("issue show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ShowArgs{}, err
		}
		return ShowArgs{}, &UsageError{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return ShowArgs{}, &UsageError{"expected exactly one issue ID"}
	}
	id, err := NormalizeID(rest[0])
	if err != nil {
		return ShowArgs{}, &UsageError{err.Error()}
	}
	return ShowArgs{ID: id}, nil
}

// MoveArgs is the parsed "za issue move" invocation.
type MoveArgs struct {
	ID     string
	To     Status
	Commit string
}

// ParseMoveArgs parses "za issue move" arguments; flags may appear before
// or after the positional ID and state.
func ParseMoveArgs(args []string) (MoveArgs, error) {
	fs := flag.NewFlagSet("issue move", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	commit := fs.String("commit", "", "landing commit SHA recorded when moving to done")

	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return MoveArgs{}, err
			}
			return MoveArgs{}, &UsageError{err.Error()}
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
	if len(positional) != 2 {
		return MoveArgs{}, &UsageError{"expected exactly one issue ID and one target state"}
	}
	id, err := NormalizeID(positional[0])
	if err != nil {
		return MoveArgs{}, &UsageError{err.Error()}
	}
	var req MoveArgs
	req.ID, req.Commit = id, *commit
	switch to := Status(positional[1]); to {
	case Todo, Doing, Done:
		req.To = to
	default:
		return req, &UsageError{fmt.Sprintf("unknown state %q (want todo, doing or done)", positional[1])}
	}
	return req, nil
}

// BoardArgs is the parsed "za board" invocation.
type BoardArgs struct {
	Project string
}

// ParseBoardArgs parses "za board" arguments.
func ParseBoardArgs(args []string) (BoardArgs, error) {
	fs := flag.NewFlagSet("board", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "only issues naming +<id>")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return BoardArgs{}, err
		}
		return BoardArgs{}, &UsageError{err.Error()}
	}
	if rest := fs.Args(); len(rest) > 0 {
		return BoardArgs{}, &UsageError{"unexpected arguments after the flags"}
	}
	return BoardArgs{Project: *project}, nil
}
