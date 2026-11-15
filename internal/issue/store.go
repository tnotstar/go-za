package issue

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tnotstar/go-za/internal/manifest"
)

// Root returns the workspace root enclosing start for issue operations. It
// walks upwards to the nearest za.toml (the same rule the root AGENTS.md
// documents), rejects an invalid or unsupported manifest, and fails closed
// when the .za/issues/ directory is missing.
func Root(start string) (string, error) {
	root, err := manifest.Find(start)
	if err != nil {
		return "", err
	}
	if _, err := manifest.Load(root); err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(Dir)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%s is missing in the workspace at %s; run \"za init\" or scaffold the .za/ layer first", Dir, root)
	case err != nil:
		return "", fmt.Errorf("inspect %s: %w", filepath.Join(root, Dir), err)
	case !info.IsDir():
		return "", fmt.Errorf("%s is not a directory in the workspace at %s", Dir, root)
	}
	return root, nil
}

// IssuesDir returns the absolute path of the issue directory of root.
func IssuesDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(Dir))
}

// RecordPath returns the workspace-relative path of the record for id.
func RecordPath(id string) string {
	return path.Join(Dir, id+".md")
}

// DefaultBody is the built-in lightweight XP skeleton used by
// "za issue add" and by "za issue new" when the workspace carries no issue
// template. It starts with the blank line that separates the record line
// from the body.
const DefaultBody = "\n## Problem & Goal\n\nTo be clarified.\n\n## Acceptance Criteria\n\n- AC-01: ...\n\n## Implementation & Verification Notes\n"

// Add creates an issue record from a parsed add/new request: it validates
// the named projects against the workspace, then allocates the next ID and
// writes the record with body. It returns the created record with its
// allocated ID.
func Add(root string, req AddArgs, body string, now time.Time) (Issue, error) {
	if err := validateProjects(root, req.Issue.Projects); err != nil {
		return Issue{}, err
	}
	return Create(IssuesDir(root), req.Issue, body, now)
}

// TemplateBody returns the body for a new record: the workspace template's
// shape when .za/templates/issue.md exists (its first line is the grammar
// placeholder and is dropped), otherwise the built-in DefaultBody.
func TemplateBody(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(TemplatePath)))
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultBody, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", TemplatePath, err)
	}
	_, rest, _ := strings.Cut(string(data), "\n")
	return rest, nil
}

// Create allocates the next issue ID in issuesDir and writes the record:
// the canonical todo.txt line, a blank line and body. It never overwrites
// an existing file. The created date is taken from now (UTC).
func Create(issuesDir string, iss Issue, body string, now time.Time) (Issue, error) {
	switch iss.Status {
	case Todo, Doing:
	default:
		return Issue{}, fmt.Errorf("new issues start as @todo or @doing, not @%s", iss.Status)
	}
	if iss.Created == "" {
		iss.Created = now.UTC().Format("2006-01-02")
	}
	iss.Completed = ""
	iss.Commit = ""
	id, err := NextID(issuesDir)
	if err != nil {
		return Issue{}, err
	}
	iss.ID = id
	if iss.Title == "" {
		return Issue{}, fmt.Errorf("missing title")
	}
	// The body carries the blank line 2: it starts with a newline (the
	// DefaultBody and the workspace template both do), and the separator
	// after the record line completes the "line 2 is blank" shape.
	if !strings.HasPrefix(body, "\n") {
		body = "\n" + body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	content := Serialize(iss) + "\n" + body
	p := filepath.Join(issuesDir, iss.ID+".md")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Issue{}, fmt.Errorf("create %s: %w", RecordPath(iss.ID), err)
	}
	if _, werr := f.WriteString(content); werr != nil {
		return Issue{}, errors.Join(werr, f.Close())
	}
	if err := f.Close(); err != nil {
		return Issue{}, err
	}
	return iss, nil
}

// LoadOne parses the record line of id in issuesDir.
func LoadOne(issuesDir, id string) (Issue, error) {
	iss, err := readHeader(filepath.Join(issuesDir, id+".md"), idNumber(id))
	if err != nil {
		return Issue{}, err
	}
	return iss, nil
}

// ReadRecord returns the full content of the record for id.
func ReadRecord(issuesDir, id string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(issuesDir, id+".md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("issue %s does not exist (%s)", id, RecordPath(id))
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", RecordPath(id), err)
	}
	return data, nil
}

func idNumber(id string) string {
	_, digits, _ := strings.Cut(id, "-")
	return digits
}

// Move rewrites the todo.txt first line of the record id to status to while
// preserving every byte of the body (line 2 onwards). Moving to done sets
// the completion date to now (UTC) and records commit when one is given;
// reopening strips the completion prefix and the landing commit. The write
// is atomic: the new content lands via a same-directory rename.
func Move(issuesDir, id string, to Status, commit string, now time.Time) (Issue, error) {
	if to != Todo && to != Doing && to != Done {
		return Issue{}, fmt.Errorf("unknown status %q (want todo, doing or done)", to)
	}
	p := filepath.Join(issuesDir, id+".md")
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Issue{}, fmt.Errorf("issue %s does not exist (%s)", id, RecordPath(id))
	}
	if err != nil {
		return Issue{}, fmt.Errorf("read %s: %w", RecordPath(id), err)
	}
	line1, rest, hasSeparator := strings.Cut(string(data), "\n")
	// The blank-line-2 shape is validated before any rewrite, so a
	// malformed record is repaired by hand, never silently rewritten.
	if hasSeparator && rest != "" && !strings.HasPrefix(rest, "\n") {
		return Issue{}, fmt.Errorf("%s: line 2 must be blank", p)
	}
	iss, err := Parse(line1)
	if err != nil {
		return Issue{}, fmt.Errorf("%s: %w", p, err)
	}
	if iss.ID != id {
		return Issue{}, fmt.Errorf("%s: the file name does not match the id tag %q", p, iss.ID)
	}

	if to == Done {
		iss.Completed = now.UTC().Format("2006-01-02")
		iss.Status = Done
		if commit != "" {
			iss.Commit = commit
		}
	} else {
		iss.Completed = ""
		iss.Commit = ""
		iss.Status = to
	}
	// rest is the exact byte sequence after the record line's newline, so
	// the recomposed file preserves the body byte for byte.
	content := Serialize(iss)
	if hasSeparator {
		content += "\n" + rest
	}

	tmp, err := os.CreateTemp(issuesDir, "."+id+".*.tmp")
	if err != nil {
		return Issue{}, fmt.Errorf("stage %s: %w", RecordPath(id), err)
	}
	tmpName := tmp.Name()
	if err := errors.Join(tmp.Chmod(0o644), writeAll(tmp, content), tmp.Close()); err != nil {
		os.Remove(tmpName)
		return Issue{}, fmt.Errorf("stage %s: %w", RecordPath(id), err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return Issue{}, fmt.Errorf("rewrite %s: %w", RecordPath(id), err)
	}
	return iss, nil
}

func writeAll(f *os.File, content string) error {
	_, err := f.WriteString(content)
	return err
}

// validateProjects checks every named project of a new issue: the
// workspace itself ("workspace") is always allowed; any other project must
// be registered in .gitmodules or exist as a top-level directory of the
// workspace root.
func validateProjects(root string, projects []string) error {
	for _, p := range projects {
		if p == "workspace" {
			continue
		}
		registered, err := projectRegistered(root, p)
		if err != nil {
			return err
		}
		if registered {
			continue
		}
		if info, err := os.Lstat(filepath.Join(root, p)); err == nil && info.IsDir() {
			continue
		}
		return fmt.Errorf("project %q is not registered in .gitmodules and no top-level directory %s exists", p, p)
	}
	return nil
}

// projectRegistered reports whether .gitmodules of root registers a
// submodule named or mounted at id.
func projectRegistered(root, id string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read .gitmodules: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[submodule") {
			name := strings.Trim(strings.TrimPrefix(line, "[submodule"), " \t[]\"")
			if name == id {
				return true, nil
			}
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == "path" {
			if strings.TrimSpace(value) == id {
				return true, nil
			}
		}
	}
	return false, nil
}
