package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Manifest
		err  string
	}{
		{name: "generated go", in: "schema = 3\n\n[workspace]\ngo = true\npython = false\nrust = false\n", want: Manifest{3, Workspace{Go: true}}},
		{name: "generated rust-only", in: "schema = 3\n\n[workspace]\ngo = false\npython = false\nrust = true\n", want: Manifest{3, Workspace{Rust: true}}},
		{name: "generated polyglot", in: "schema = 3\n\n[workspace]\ngo = true\npython = true\nrust = true\n", want: Manifest{3, Workspace{Go: true, Python: true, Rust: true}}},
		{name: "missing rust reads as false", in: "schema = 3\n\n[workspace]\ngo = true\npython = false\n", want: Manifest{3, Workspace{Go: true}}},
		{name: "whitespace and comments", in: "# za\n  schema=3   # layout\n[ workspace ]\n\tpython = false\n go=true#x\n", want: Manifest{3, Workspace{Go: true}}},
		{name: "crlf", in: "schema = 3\r\n[workspace]\r\ngo = true\r\npython = false\r\n", want: Manifest{3, Workspace{Go: true}}},
		{name: "missing schema", in: "[workspace]\ngo = true\npython = false\n", err: "missing schema"},
		{name: "unsupported schema 2", in: "schema = 2\n[workspace]\ngo = true\npython = false\n", err: "unsupported za workspace schema: 2"},
		{name: "unsupported schema 1", in: "schema = 1\n[workspace]\ngo = true\npython = false\n", err: "unsupported za workspace schema: 1"},
		{name: "missing workspace", in: "schema = 3\n", err: "missing [workspace] table"},
		{name: "missing go", in: "schema = 3\n[workspace]\npython = false\n", err: "missing workspace.go"},
		{name: "missing python", in: "schema = 3\n[workspace]\ngo = true\n", err: "missing workspace.python"},
		{name: "schema inside table", in: "[workspace]\nschema = 3\ngo = true\npython = false\n", err: "unknown key workspace.schema"},
		{name: "unknown key", in: "schema = 3\n[workspace]\ngo = true\npython = false\nrust = maybe\n", err: "invalid boolean"},
		{name: "unknown key rejected", in: "schema = 3\n[workspace]\ngo = true\npython = false\nruby = true\n", err: "unknown key workspace.ruby"},
		{name: "unknown table", in: "schema = 3\n[projects]\n", err: "unknown table [projects]"},
		{name: "array table", in: "schema = 3\n[[workspace]]\n", err: "unsupported table header"},
		{name: "duplicate table", in: "schema = 3\n[workspace]\ngo = true\npython = false\n[workspace]\n", err: "duplicate table"},
		{name: "duplicate key", in: "schema = 3\n[workspace]\ngo = true\ngo = false\npython = false\n", err: "duplicate key workspace.go"},
		{name: "quoted boolean", in: "schema = 3\n[workspace]\ngo = \"true\"\npython = false\n", err: "invalid boolean"},
		{name: "capitalized boolean", in: "schema = 3\n[workspace]\ngo = True\npython = false\n", err: "invalid boolean"},
		{name: "string schema", in: "schema = \"1\"\n[workspace]\ngo = true\npython = false\n", err: "invalid integer"},
		{name: "leading zero schema", in: "schema = 01\n[workspace]\ngo = true\npython = false\n", err: "invalid integer"},
		{name: "not key value", in: "schema 1\n", err: "line 1: expected key = value"},
		{name: "dotted key", in: "schema = 3\nworkspace.go = true\n", err: "only bare keys are allowed"},
		{name: "quoted key", in: "schema = 3\n[workspace]\n\"go\" = true\n", err: "only bare keys are allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.in))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUnsupportedSchemaIsTyped(t *testing.T) {
	_, err := Parse([]byte("schema = 7\n"))
	var se *UnsupportedSchemaError
	if !errors.As(err, &se) || se.Schema != 7 {
		t.Fatalf("err = %v, want *UnsupportedSchemaError{7}", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind(t *testing.T) {
	outer := t.TempDir()
	write(t, filepath.Join(outer, FileName), "schema = 3\n")
	inner := filepath.Join(outer, "a", "inner")
	write(t, filepath.Join(inner, FileName), "schema = 3\n")
	deep := filepath.Join(inner, "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// go.work and AGENTS.md alone never identify a workspace.
	bare := filepath.Join(t.TempDir(), "bare")
	write(t, filepath.Join(bare, "go.work"), "go 1.24\n")
	write(t, filepath.Join(bare, "AGENTS.md"), "x\n")
	if err := os.Mkdir(filepath.Join(bare, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, start, want string
		notFound          bool
	}{
		{name: "root", start: outer, want: outer},
		{name: "child of outer", start: filepath.Join(outer, "a"), want: outer},
		{name: "nearest wins", start: deep, want: inner},
		{name: "no manifest", start: bare, notFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Find(tt.start)
			if tt.notFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Find(%s) = %q, %v; want %q", tt.start, got, err, tt.want)
			}
		})
	}
}

func TestFindRejectsNonRegularManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "read workspace manifest") {
		t.Fatalf("missing manifest: err = %v", err)
	}
	write(t, filepath.Join(dir, FileName), "schema = 3\n[workspace]\ngo = false\npython = true\n")
	m, err := Load(dir)
	if err != nil || m.Workspace.Go || !m.Workspace.Python {
		t.Fatalf("Load = %+v, %v", m, err)
	}
}
