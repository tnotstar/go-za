// Package manifest locates and reads za.toml, the za workspace manifest.
//
// Only schema 3 exists: the flat context layout with a context-root
// INDEX.md router. The parser accepts exactly the TOML subset that
// schema 3 needs: comments, blank lines, the [workspace] table header and
// bare keys with integer or boolean values. Anything else, including unknown
// keys and duplicates, is rejected rather than guessed at. Schemas 1 and 2
// describe different, older layouts; they are rejected fail-closed, and no
// migration exists.
package manifest

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the manifest file at the root of every za workspace.
const FileName = "za.toml"

// SupportedSchema is the only workspace schema this za version understands.
const SupportedSchema = 3

// ErrNotFound reports that no za.toml encloses the starting directory.
var ErrNotFound = errors.New("not inside a za workspace")

// Manifest is the typed content of a schema 3 za.toml.
type Manifest struct {
	Schema    int
	Workspace Workspace
}

// Workspace is the [workspace] table.
type Workspace struct {
	Go     bool
	Python bool
	Rust   bool
}

// Find returns the directory of the nearest za.toml at or above start. It
// stops at the filesystem root and returns ErrNotFound if there is none.
// The nearest manifest wins even if it turns out to be invalid; an outer
// workspace is never selected silently.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", start, err)
	}
	for {
		info, err := os.Lstat(filepath.Join(dir, FileName))
		switch {
		case err == nil && info.Mode().IsRegular():
			return dir, nil
		case err == nil:
			return "", fmt.Errorf("%s is not a regular file", filepath.Join(dir, FileName))
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("inspect %s: %w", filepath.Join(dir, FileName), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Load reads and parses root/za.toml.
func Load(root string) (Manifest, error) {
	path := filepath.Join(root, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read workspace manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("invalid workspace manifest %s: %w", path, err)
	}
	return m, nil
}

// Parse parses a schema 3 manifest. Newly generated manifests record all
// three capability booleans; reading an otherwise valid manifest without
// workspace.rust means Rust is disabled, which keeps manifests written by
// earlier schema-3 writers readable.
func Parse(data []byte) (Manifest, error) {
	m, seen, err := parseLines(data)
	if err != nil {
		return Manifest{}, err
	}
	switch {
	case !seen["schema"]:
		return Manifest{}, errors.New("missing schema")
	case m.Schema != SupportedSchema:
		return Manifest{}, &UnsupportedSchemaError{Schema: m.Schema}
	case !seen["[workspace]"]:
		return Manifest{}, errors.New("missing [workspace] table")
	case !seen["workspace.go"]:
		return Manifest{}, errors.New("missing workspace.go")
	case !seen["workspace.python"]:
		return Manifest{}, errors.New("missing workspace.python")
	}
	return m, nil
}

// parseLines decodes every line and returns the set of tables ("[name]")
// and qualified keys it saw.
func parseLines(data []byte) (Manifest, map[string]bool, error) {
	var m Manifest
	seen := map[string]bool{}
	table := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		fail := func(format string, a ...any) (Manifest, map[string]bool, error) {
			return Manifest{}, nil, fmt.Errorf("line %d: %s", lineNo, fmt.Sprintf(format, a...))
		}
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || strings.HasPrefix(line, "[[") {
				return fail("unsupported table header %q", line)
			}
			table = strings.TrimSpace(line[1 : len(line)-1])
			if table != "workspace" {
				return fail("unknown table [%s]", table)
			}
			if seen["["+table+"]"] {
				return fail("duplicate table [%s]", table)
			}
			seen["["+table+"]"] = true
			continue
		}

		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return fail("expected key = value, got %q", line)
		}
		key, raw = strings.TrimSpace(key), strings.TrimSpace(raw)
		if !isBareKey(key) {
			return fail("unsupported key %q: only bare keys are allowed", key)
		}
		if table != "" {
			key = table + "." + key
		}
		if seen[key] {
			return fail("duplicate key %s", key)
		}
		seen[key] = true

		var err error
		switch key {
		case "schema":
			m.Schema, err = parseInt(raw)
		case "workspace.go":
			m.Workspace.Go, err = parseBool(raw)
		case "workspace.python":
			m.Workspace.Python, err = parseBool(raw)
		case "workspace.rust":
			m.Workspace.Rust, err = parseBool(raw)
		default:
			return fail("unknown key %s", key)
		}
		if err != nil {
			return fail("%s: %v", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, nil, err
	}
	return m, seen, nil
}

// UnsupportedSchemaError reports a manifest written for another schema.
type UnsupportedSchemaError struct {
	Schema int
}

func (e *UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("unsupported za workspace schema: %d (this za version supports schema %d only; there is no migration)", e.Schema, SupportedSchema)
}

// stripComment removes a trailing comment. Schema 3 has no string values, so
// "#" never appears inside a value.
func stripComment(line string) string {
	before, _, _ := strings.Cut(line, "#")
	return before
}

// isBareKey reports whether key is a non-empty TOML bare key, which excludes
// quoted and dotted keys.
func isBareKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func parseInt(raw string) (int, error) {
	if raw == "" || strings.ContainsAny(raw, "_+") || (len(raw) > 1 && raw[0] == '0') {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	return v, nil
}

func parseBool(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", raw)
}
