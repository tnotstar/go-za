// Rust-side onboarding for "za project add". za registers only single-root
// crates: a repository whose root Cargo.toml declares [package] without
// [workspace] joins the workspace's Cargo workspace as a member. A
// repository that is itself a Cargo workspace root cannot be a member —
// Cargo rejects nested workspace roots — so za rejects it with an
// actionable diagnostic instead of guessing.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// cargoManifest carries only what za checks in a Cargo.toml: the presence of
// the [workspace] and [package] tables and the workspace member list.
// Tables decode to nil when absent, so even an empty [workspace] table is
// visible to the single-root checks.
type cargoManifest struct {
	Workspace *struct {
		Members []string `toml:"members"`
	} `toml:"workspace"`
	Package *struct {
		Name    string `toml:"name"`
		Edition string `toml:"edition"`
	} `toml:"package"`
}

func parseCargoManifest(path string) (cargoManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cargoManifest{}, err
	}
	return decodeCargoManifest(data)
}

func decodeCargoManifest(data []byte) (cargoManifest, error) {
	var m cargoManifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return cargoManifest{}, fmt.Errorf("parse TOML: %w", err)
	}
	return m, nil
}

// scanNestedManifests lists Cargo.toml files strictly below dir, relative,
// slash-separated and sorted, skipping .git. A nested manifest would be an
// unregistered crate or a nested workspace root; both are rejected, so the
// walk names the offending paths instead of failing silently.
func scanNestedManifests(dir string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Cargo.toml" && p != filepath.Join(dir, "Cargo.toml") {
			rel, rerr := filepath.Rel(dir, p)
			if rerr != nil {
				return rerr
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	slices.Sort(found)
	return found, nil
}

// addWorkspaceMember inserts id into the members array of the [workspace]
// table of root's Cargo.toml, keeping the array lexically sorted. The edit
// is textual: comments and formatting outside the members array survive
// byte for byte. The result is re-parsed and must decode to exactly the
// previous members plus id; any shape za cannot edit safely fails closed
// rather than guessing.
func addWorkspaceMember(root, id string) error {
	path := filepath.Join(root, "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the workspace Cargo.toml: %w", err)
	}
	m, err := decodeCargoManifest(data)
	if err != nil {
		return fmt.Errorf("the workspace Cargo.toml is not valid: %w", err)
	}
	if m.Workspace == nil {
		return errors.New("the workspace Cargo.toml has no [workspace] table; recreate the workspace with \"za init --rust\"")
	}
	members := m.Workspace.Members
	if slices.Contains(members, id) {
		return fmt.Errorf("the workspace Cargo.toml already lists %q as a member", id)
	}

	edited, err := insertMember(string(data), id, members)
	if err != nil {
		return err
	}

	// Prove the edit changed exactly the member set before writing.
	want := slices.Clone(members)
	want = append(want, id)
	slices.Sort(want)
	check, err := decodeCargoManifest([]byte(edited))
	if err != nil {
		return fmt.Errorf("the edited workspace Cargo.toml is not valid: %w", err)
	}
	if check.Workspace == nil {
		return errors.New("the edited workspace Cargo.toml lost its [workspace] table")
	}
	got := slices.Clone(check.Workspace.Members)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("the members edit changed more than intended: got %v, want %v", check.Workspace.Members, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(edited), info.Mode().Perm())
}

// insertMember returns data with id inserted into the [workspace] members
// array in lexical order. members are the decoded current entries in file
// order. Only verifiable shapes are edited: a single-line array, or a
// multi-line array whose entries are one plain quoted string per line.
// Anything else fails closed.
func insertMember(data, id string, members []string) (string, error) {
	quotedID, err := quoteMember(id)
	if err != nil {
		return "", err
	}
	lines := strings.Split(data, "\n")
	open, close, err := locateMembers(lines)
	if err != nil {
		return "", err
	}

	if open == close {
		// A single-line array carries no comments inside it (a "#"
		// would comment out the closing bracket and fail to decode),
		// so inserting between the plain entries is exact. Whatever
		// follows the first "]" is preserved as the line suffix.
		opening := lines[open]
		inner := strings.TrimSpace(opening[strings.Index(opening, "[")+1:])
		body, suffix, _ := strings.Cut(inner, "]")
		entries, err := splitInlineEntries(strings.TrimSpace(body), members)
		if err != nil {
			return "", err
		}
		at := len(entries)
		for i, e := range entries {
			if e.raw > id {
				at = i
				break
			}
		}
		entry := inlineEntry{raw: id, text: quotedID}
		entries = slices.Insert(entries, at, entry)
		texts := make([]string, len(entries))
		for i, e := range entries {
			texts[i] = e.text
		}
		lines[open] = leadingSpace(opening) + "members = [" + strings.Join(texts, ", ") + "]" + suffix
		return strings.Join(lines, "\n"), nil
	}

	// Multi-line array: the opening line ends with "[", the entries are
	// one plain quoted string per line, and the closing line holds "]".
	// Entry and comment lines in between keep their bytes; only the new
	// line is added.
	entryLines := lines[open+1 : close]
	if len(entryLines) != len(members) {
		return "", errors.New("unsupported members array: entries are not one plain quoted string per line")
	}
	insertAt, indent := close, leadingSpace(lines[open])+"  "
	for i, line := range entryLines {
		raw, ok := leadingQuoted(line)
		if !ok || raw != members[i] {
			return "", fmt.Errorf("unsupported members array: entry %d is not the plain quoted string %q", i+1, members[i])
		}
		if members[i] > id && insertAt == close {
			insertAt, indent = open+1+i, leadingSpace(line)
		}
	}
	lines = slices.Insert(lines, insertAt, indent+quotedID+",")
	return strings.Join(lines, "\n"), nil
}

// locateMembers finds the line indexes of the "members = [" assignment
// inside the [workspace] table, and of the line closing the array. When
// both live on one line the indexes are equal. The [workspace] header must
// be in canonical form; anything za cannot locate exactly fails closed.
func locateMembers(lines []string) (open, close int, err error) {
	inWorkspace := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if trimmed == "[workspace]" {
				if inWorkspace {
					return 0, 0, errors.New("the workspace Cargo.toml declares [workspace] more than once")
				}
				inWorkspace = true
				continue
			}
			if inWorkspace {
				// The next table header ends [workspace] without a
				// members array in it.
				break
			}
			continue
		}
		if !inWorkspace {
			continue
		}
		key, rest, ok := strings.Cut(trimmed, "=")
		rest = strings.TrimSpace(rest)
		if !ok || strings.TrimSpace(key) != "members" || !strings.HasPrefix(rest, "[") {
			continue
		}
		if strings.Contains(rest, "]") {
			return i, i, nil
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.Contains(lines[j], "]") {
				return i, j, nil
			}
		}
		return 0, 0, errors.New("the members array in the workspace Cargo.toml is not closed")
	}
	return 0, 0, errors.New("the workspace Cargo.toml has no members array in an editable form; keep a canonical \"[workspace]\" table with a \"members = [...]\" assignment")
}

// inlineEntry is one entry of a single-line members array: raw is the
// member value and text its quoted form, kept separate so sorting compares
// values, not their quoting.
type inlineEntry struct{ raw, text string }

// splitInlineEntries splits the body of a single-line members array into
// its entries and checks them against the decoded members.
func splitInlineEntries(body string, members []string) ([]inlineEntry, error) {
	if body == "" {
		if len(members) != 0 {
			return nil, errors.New("unsupported members array: decoded entries not found in the array")
		}
		return nil, nil
	}
	parts := strings.Split(body, ",")
	if len(parts) != len(members) {
		return nil, errors.New("unsupported members array: entries are not one plain quoted string each")
	}
	entries := make([]inlineEntry, len(parts))
	for i, part := range parts {
		raw, ok := leadingQuoted(part)
		if !ok || raw != members[i] {
			return nil, fmt.Errorf("unsupported members array: entry %d is not the plain quoted string %q", i+1, members[i])
		}
		entries[i] = inlineEntry{raw: raw, text: `"` + raw + `"`}
	}
	return entries, nil
}

// isCrateName reports whether s is a plain Cargo crate name: identifiers of
// letters, digits, underscore and hyphen. Values outside that set never
// reach rendered context files or the success summary.
func isCrateName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// isEdition reports whether s is a plain edition label such as "2015",
// "2018", "2021" or "2024". The value is rendered into a context table row,
// so anything beyond digits and dots fails closed.
func isEdition(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r == '.') {
			return false
		}
	}
	return true
}

// leadingQuoted returns the raw inner text of a line whose first
// non-space character opens a double-quoted string without escapes.
func leadingQuoted(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, `"`) {
		return "", false
	}
	rest := trimmed[1:]
	end := strings.Index(rest, `"`)
	if end < 0 || strings.ContainsAny(rest[:end], "\\") {
		return "", false
	}
	return rest[:end], true
}

// quoteMember renders s as a double-quoted TOML string. Workspace member
// entries added by za are project IDs, whose validated character set needs
// no escaping; anything else fails closed.
func quoteMember(s string) (string, error) {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return "", fmt.Errorf("unsupported workspace member name %q", s)
		}
	}
	return `"` + s + `"`, nil
}

// leadingSpace returns the leading blank prefix of line.
func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
