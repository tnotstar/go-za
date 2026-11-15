// Package issue implements the workspace issue records of the .za/ layer
// and the "za issue" and "za board" subcommands.
//
// An issue record is one Markdown file .za/issues/I-NNNNNN.md whose first
// line is a canonical todo.txt line carrying the machine-readable state and
// whose body holds lightweight XP sections. Line 2 is blank:
//
//	Active:    [(Priority)] YYYY-MM-DD <Title> +<project> @<todo|doing> id:I-NNNNNN [spec:<path>] [adr:D-NNNN]
//	Completed: x YYYY-MM-DD YYYY-MM-DD [(Priority)] <Title> +<project> @done id:I-NNNNNN [commit:<sha>]
//
// Parsing is strict: unknown or duplicated mandatory tags, malformed dates
// and a @done tag without the "x" prefix (or the reverse) are errors, never
// guesses. Serialization is canonical: it always emits the field order
// above, so a rewritten first line is deterministic even when the input
// ordered its tags differently.
package issue

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dir is the workspace-relative directory holding the issue records.
const Dir = ".za/issues"

// TemplatePath is the workspace-relative path of the canonical issue
// template used by "za issue new".
const TemplatePath = ".za/templates/issue.md"

// Status is the workflow state carried by the @tag on line 1.
type Status string

// The three issue states.
const (
	Todo  Status = "todo"
	Doing Status = "doing"
	Done  Status = "done"
)

// Issue is the parsed todo.txt first line of one issue record.
type Issue struct {
	ID        string // I-NNNNNN, matching the record file name
	Status    Status // exactly one of todo, doing, done
	Priority  byte   // 'A'..'Z', or 0 when the line carries no priority
	Created   string // YYYY-MM-DD
	Completed string // YYYY-MM-DD; done records only
	Title     string
	Projects  []string // +project tags in line order
	Spec      string   // spec:<wiki-path>, empty when absent
	ADR       string   // adr:D-NNNN, empty when absent
	Commit    string   // commit:<sha>, empty when absent
	Extra     []string // unknown key:value tags, in line order
}

var (
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	idPattern     = regexp.MustCompile(`^I-\d{6}$`)
	numberPattern = regexp.MustCompile(`^\d{1,6}$`)
	prioPattern   = regexp.MustCompile(`^\(([A-Z])\)$`)
	filePattern   = regexp.MustCompile(`^I-(\d{6})\.md$`)
)

// FormatID renders the sequential issue ID for n.
func FormatID(n int) string { return fmt.Sprintf("I-%06d", n) }

// NormalizeID accepts I-NNNNNN and the numeric shorthands N and 0N...0N,
// returning the canonical I-NNNNNN form.
func NormalizeID(s string) (string, error) {
	if idPattern.MatchString(s) {
		return s, nil
	}
	if numberPattern.MatchString(s) {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return FormatID(n), nil
		}
	}
	return "", fmt.Errorf("malformed issue ID %q (want I-NNNNNN or a number 1..999999)", s)
}

// Parse parses the canonical todo.txt line.
func Parse(line string) (Issue, error) {
	iss, _, err := parseLine(line, false)
	return iss, err
}

// ParseAllocating parses a canonical todo.txt line whose id: tag is
// optional, as accepted by "za issue add" for raw line input.
func ParseAllocating(line string) (Issue, error) {
	iss, _, err := parseLine(line, true)
	return iss, err
}

func isDate(s string) bool {
	if !datePattern.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// parseLine parses one canonical line. In lenient mode the id: tag is
// optional (a new record gets its ID from the allocator, not from the
// input). The second result reports whether the line carried an id: tag.
func parseLine(line string, lenient bool) (Issue, bool, error) {
	var iss Issue
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return iss, false, fmt.Errorf("empty line")
	}
	pos := 0
	done := fields[0] == "x"
	if done {
		pos++
		if len(fields)-pos < 2 {
			return iss, false, fmt.Errorf("done line lacks the completion and creation dates")
		}
		if !isDate(fields[pos]) {
			return iss, false, fmt.Errorf("malformed completion date %q", fields[pos])
		}
		iss.Completed = fields[pos]
		pos++
		if !isDate(fields[pos]) {
			return iss, false, fmt.Errorf("malformed creation date %q", fields[pos])
		}
		iss.Created = fields[pos]
		pos++
		if pos < len(fields) {
			if m := prioPattern.FindStringSubmatch(fields[pos]); m != nil {
				iss.Priority = m[1][0]
				pos++
			}
		}
	} else {
		if m := prioPattern.FindStringSubmatch(fields[pos]); m != nil {
			iss.Priority = m[1][0]
			pos++
		}
		if pos >= len(fields) || !isDate(fields[pos]) {
			return iss, false, fmt.Errorf("lacks a creation date (YYYY-MM-DD)")
		}
		iss.Created = fields[pos]
		pos++
	}

	// The tag region is the maximal trailing run of tag-shaped tokens;
	// everything before it is the title.
	rest := fields[pos:]
	end := len(rest)
	for end > 0 && isTagToken(rest[end-1]) {
		end--
	}
	titleTokens, tags := rest[:end], rest[end:]

	statusSeen, idSeen := false, false
	for _, tok := range tags {
		switch {
		case strings.HasPrefix(tok, "+"):
			name := tok[1:]
			if !validProjectName(name) {
				return iss, false, fmt.Errorf("malformed project tag %q", tok)
			}
			iss.Projects = append(iss.Projects, name)
		case strings.HasPrefix(tok, "@"):
			st := Status(tok[1:])
			if st != Todo && st != Doing && st != Done {
				return iss, false, fmt.Errorf("unknown status tag %q (want @todo, @doing or @done)", tok)
			}
			if statusSeen {
				return iss, false, fmt.Errorf("multiple status tags")
			}
			iss.Status, statusSeen = st, true
		default:
			key, value, _ := strings.Cut(tok, ":")
			if value == "" {
				return iss, false, fmt.Errorf("malformed tag %q (empty value)", tok)
			}
			switch key {
			case "id":
				if idSeen {
					return iss, false, fmt.Errorf("multiple id tags")
				}
				if !idPattern.MatchString(value) {
					return iss, false, fmt.Errorf("malformed id tag %q (want id:I-NNNNNN)", tok)
				}
				iss.ID, idSeen = value, true
			case "spec":
				iss.Spec = value
			case "adr":
				iss.ADR = value
			case "commit":
				iss.Commit = value
			default:
				iss.Extra = append(iss.Extra, tok)
			}
		}
	}
	iss.Title = strings.Join(titleTokens, " ")

	if iss.Title == "" {
		return iss, idSeen, fmt.Errorf("missing title")
	}
	if !statusSeen {
		return iss, idSeen, fmt.Errorf("missing status tag (@todo, @doing or @done)")
	}
	if !lenient && !idSeen {
		return iss, idSeen, fmt.Errorf("missing id:I-NNNNNN tag")
	}
	switch {
	case done && iss.Status != Done:
		return iss, idSeen, fmt.Errorf("completed line carries %q instead of @done", iss.Status)
	case !done && iss.Status == Done:
		return iss, idSeen, fmt.Errorf("@done requires the completed \"x\" line form")
	}
	return iss, idSeen, nil
}

// isTagToken reports whether tok is shaped like a trailing tag: a +project,
// an @status or a key:value token.
func isTagToken(tok string) bool {
	switch {
	case strings.HasPrefix(tok, "+"):
		return true
	case strings.HasPrefix(tok, "@"):
		return true
	}
	key, _, found := strings.Cut(tok, ":")
	return found && key != ""
}

// validProjectName accepts the same portable name shape as project IDs:
// ASCII letters, digits, ".", "_" and "-", non-empty.
func validProjectName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// Serialize renders the canonical todo.txt line for i.
func Serialize(i Issue) string {
	var b strings.Builder
	if i.Status == Done {
		b.WriteString("x ")
		b.WriteString(i.Completed)
		b.WriteString(" ")
		b.WriteString(i.Created)
		b.WriteString(" ")
		if i.Priority != 0 {
			fmt.Fprintf(&b, "(%c) ", i.Priority)
		}
	} else {
		if i.Priority != 0 {
			fmt.Fprintf(&b, "(%c) ", i.Priority)
		}
		b.WriteString(i.Created)
		b.WriteString(" ")
	}
	b.WriteString(i.Title)
	for _, p := range i.Projects {
		b.WriteString(" +" + p)
	}
	fmt.Fprintf(&b, " @%s id:%s", i.Status, i.ID)
	if i.Spec != "" {
		b.WriteString(" spec:" + i.Spec)
	}
	if i.ADR != "" {
		b.WriteString(" adr:" + i.ADR)
	}
	if i.Commit != "" {
		b.WriteString(" commit:" + i.Commit)
	}
	for _, e := range i.Extra {
		b.WriteString(" " + e)
	}
	return b.String()
}
