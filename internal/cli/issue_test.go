package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tnotstar/go-za/internal/tool/tooltest"
)

// issueWorkspace builds a minimal valid za workspace with an issue
// directory, a registered project and an issue template.
func issueWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"za.toml":                "schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n",
		".gitmodules":            "[submodule \"go-za\"]\n\tpath = go-za\n\turl = https://example.invalid/go-za.git\n",
		".za/templates/issue.md": "[(A|B|C)] YYYY-MM-DD <Title> +<project-id> @todo id:I-NNNNNN\n\n## Problem & Goal\n\nTo be clarified.\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".za", "issues"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunIssueCommands(t *testing.T) {
	root := issueWorkspace(t)
	inRoot := func() (string, error) { return root, nil }
	fixedNow := func() time.Time { return time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC) }

	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		f := &tooltest.Fake{Effect: tooltest.Native}
		env := Env{Runner: f, Getwd: inRoot, Now: fixedNow}
		code := Run(t.Context(), args, &stdout, &stderr, env)
		return code, stdout.String(), stderr.String()
	}

	t.Run("add with flags", func(t *testing.T) {
		code, out, errOut := run("issue", "add", "-p", "A", "-project", "go-za", "-spec", ".za/wiki/index.md", "Implement the issue CLI")
		if code != ExitOK || errOut != "" {
			t.Fatalf("add: code %d, stderr %q", code, errOut)
		}
		if out != "I-000001 .za/issues/I-000001.md\n" {
			t.Errorf("add output = %q", out)
		}
		data, err := os.ReadFile(filepath.Join(root, ".za", "issues", "I-000001.md"))
		if err != nil {
			t.Fatal(err)
		}
		want := "(A) 2026-11-15 Implement the issue CLI +go-za @todo id:I-000001 spec:.za/wiki/index.md\n\n## Problem & Goal\n\nTo be clarified.\n\n## Acceptance Criteria\n\n- AC-01: ...\n\n## Implementation & Verification Notes\n"
		if string(data) != want {
			t.Errorf("record =\n%q\nwant\n%q", data, want)
		}
	})

	t.Run("add defaults to +workspace and priority B", func(t *testing.T) {
		code, out, errOut := run("issue", "add", "Workspace-only note")
		if code != ExitOK || errOut != "" {
			t.Fatalf("add: code %d, stderr %q", code, errOut)
		}
		if out != "I-000002 .za/issues/I-000002.md\n" {
			t.Errorf("add output = %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".za", "issues", "I-000002.md"))
		if !strings.HasPrefix(string(data), "(B) 2026-11-15 Workspace-only note +workspace @todo id:I-000002") {
			t.Errorf("unexpected defaults: %q", data)
		}
	})

	t.Run("new uses the workspace template body", func(t *testing.T) {
		code, out, errOut := run("issue", "new", "-p", "A", "-project", "go-za", "Template-backed record")
		if code != ExitOK || errOut != "" {
			t.Fatalf("new: code %d, stderr %q", code, errOut)
		}
		if out != "I-000003 .za/issues/I-000003.md\n" {
			t.Errorf("new output = %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".za", "issues", "I-000003.md"))
		want := "(A) 2026-11-15 Template-backed record +go-za @todo id:I-000003\n\n## Problem & Goal\n\nTo be clarified.\n"
		if string(data) != want {
			t.Errorf("record =\n%q\nwant\n%q", data, want)
		}
	})

	t.Run("add rejects an unregistered project", func(t *testing.T) {
		code, _, errOut := run("issue", "add", "-project", "ghost", "Ghost project")
		if code != ExitFailure || !strings.Contains(errOut, "not registered in .gitmodules") {
			t.Errorf("add ghost: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("add rejects a raw line combined with flags", func(t *testing.T) {
		code, _, errOut := run("issue", "add", "-p", "A", "(C) 2026-11-14 Raw +go-za @todo")
		if code != ExitUsage || !strings.Contains(errOut, "cannot be combined with flags") {
			t.Errorf("raw+flags: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("add accepts a raw todo.txt line", func(t *testing.T) {
		code, out, errOut := run("issue", "add", "(C) 2026-11-14 Raw line +go-za @doing")
		if code != ExitOK || errOut != "" {
			t.Fatalf("raw add: code %d, stderr %q", code, errOut)
		}
		if out != "I-000004 .za/issues/I-000004.md\n" {
			t.Errorf("raw add output = %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".za", "issues", "I-000004.md"))
		if !strings.HasPrefix(string(data), "(C) 2026-11-14 Raw line +go-za @doing id:I-000004") {
			t.Errorf("unexpected raw record: %q", data)
		}
	})

	t.Run("ls sorts by priority, status and ID", func(t *testing.T) {
		code, out, errOut := run("issue", "ls")
		if code != ExitOK || errOut != "" {
			t.Fatalf("ls: code %d, stderr %q", code, errOut)
		}
		want := "I-000001\ttodo\t(A)\t2026-11-15\tgo-za\tImplement the issue CLI\n" +
			"I-000003\ttodo\t(A)\t2026-11-15\tgo-za\tTemplate-backed record\n" +
			"I-000002\ttodo\t(B)\t2026-11-15\tworkspace\tWorkspace-only note\n" +
			"I-000004\tdoing\t(C)\t2026-11-14\tgo-za\tRaw line\n"
		if out != want {
			t.Errorf("ls =\n%s\nwant\n%s", out, want)
		}
	})

	t.Run("ls filters", func(t *testing.T) {
		code, out, _ := run("issue", "ls", "-status", "doing")
		if code != ExitOK || !strings.Contains(out, "I-000004\tdoing") || strings.Contains(out, "I-000001") {
			t.Errorf("ls -status doing = %q", out)
		}
		code, out, _ = run("issue", "ls", "-project", "go-za", "-priority", "A")
		if code != ExitOK || !strings.Contains(out, "I-000001") || strings.Contains(out, "I-000002") {
			t.Errorf("ls filtered = %q", out)
		}
		code, out, _ = run("issue", "ls", "-status", "all")
		if code != ExitOK || strings.Count(out, "\n") != 4 {
			t.Errorf("ls -status all = %q", out)
		}
	})

	t.Run("show prints the record and accepts shorthands", func(t *testing.T) {
		code, out, errOut := run("issue", "show", "1")
		if code != ExitOK || errOut != "" {
			t.Fatalf("show: code %d, stderr %q", code, errOut)
		}
		if !strings.HasPrefix(out, "(A) 2026-11-15 Implement the issue CLI +go-za @todo id:I-000001") {
			t.Errorf("show output = %q", out)
		}
		code, out2, _ := run("issue", "show", "I-000001")
		if code != ExitOK || out2 != out {
			t.Errorf("show I-000001 differs from show 1")
		}
		code, _, errOut = run("issue", "show", "999")
		if code != ExitFailure || !strings.Contains(errOut, "does not exist") {
			t.Errorf("show missing: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("move transitions and board", func(t *testing.T) {
		code, out, errOut := run("issue", "move", "1", "doing")
		if code != ExitOK || errOut != "" {
			t.Fatalf("move: code %d, stderr %q", code, errOut)
		}
		if out != "(A) 2026-11-15 Implement the issue CLI +go-za @doing id:I-000001 spec:.za/wiki/index.md\n" {
			t.Errorf("move output = %q", out)
		}

		code, out, _ = run("issue", "move", "I-000001", "done", "-commit", "abc123")
		if code != ExitOK || out != "x 2026-11-15 2026-11-15 (A) Implement the issue CLI +go-za @done id:I-000001 spec:.za/wiki/index.md commit:abc123\n" {
			t.Errorf("move to done = %q (code %d)", out, code)
		}

		code, out, _ = run("issue", "move", "1", "todo")
		if code != ExitOK || !strings.HasPrefix(out, "(A) 2026-11-15 Implement the issue CLI") || strings.Contains(out, "commit:") || strings.Contains(out, "x 2026") {
			t.Errorf("reopen = %q (code %d)", out, code)
		}

		var boardOut, stderr bytes.Buffer
		f := &tooltest.Fake{Effect: tooltest.Native}
		env := Env{Runner: f, Getwd: inRoot, Now: fixedNow}
		if code := Run(t.Context(), []string{"board"}, &boardOut, &stderr, env); code != ExitOK {
			t.Fatalf("board: code %d, stderr %q", code, stderr.String())
		}
		board := boardOut.String()
		for _, section := range []string{"DOING\n  I-000004", "TODO\n  I-000001", "DONE\n"} {
			if !strings.Contains(board, section) {
				t.Errorf("board lacks %q:\n%s", section, board)
			}
		}
		if !strings.HasPrefix(board, "DOING\n") || strings.Count(board, "\n\n") != 2 {
			t.Errorf("board shape unexpected:\n%s", board)
		}

		// The body survived every transition byte for byte.
		data, err := os.ReadFile(filepath.Join(root, ".za", "issues", "I-000001.md"))
		if err != nil {
			t.Fatal(err)
		}
		body := "\n## Problem & Goal\n\nTo be clarified.\n\n## Acceptance Criteria\n\n- AC-01: ...\n\n## Implementation & Verification Notes\n"
		if !strings.HasSuffix(string(data), body) {
			t.Errorf("body not preserved after the moves:\n%q", data)
		}
	})

	t.Run("board project filter", func(t *testing.T) {
		var boardOut, stderr bytes.Buffer
		f := &tooltest.Fake{Effect: tooltest.Native}
		env := Env{Runner: f, Getwd: inRoot, Now: fixedNow}
		code := Run(t.Context(), []string{"board", "-project", "go-za"}, &boardOut, &stderr, env)
		if code != ExitOK {
			t.Fatalf("board: code %d, stderr %q", code, stderr.String())
		}
		if strings.Contains(boardOut.String(), "+workspace") || !strings.Contains(boardOut.String(), "I-000004") {
			t.Errorf("board filter = %q", boardOut.String())
		}
	})

	t.Run("usage errors exit with code 2", func(t *testing.T) {
		for _, args := range [][]string{
			{"issue"},
			{"issue", "frobnicate"},
			{"issue", "add"},
			{"issue", "add", "-p", "42", "Title"},
			{"issue", "add", "-status", "done", "Title"},
			{"issue", "add", "-adr", "nope", "Title"},
			{"issue", "ls", "-status", "sometimes"},
			{"issue", "show"},
			{"issue", "show", "zero"},
			{"issue", "move", "1"},
			{"issue", "move", "1", "sideways"},
			{"board", "extra"},
		} {
			code, out, errOut := run(args...)
			if code != ExitUsage {
				t.Errorf("args %v: code = %d, want %d (stderr %q, stdout %q)", args, code, ExitUsage, errOut, out)
			}
			if errOut == "" {
				t.Errorf("args %v: no diagnostic on stderr", args)
			}
		}
	})

	t.Run("help", func(t *testing.T) {
		code, out, _ := run("issue", "--help")
		if code != ExitOK || !strings.Contains(out, "Commands:") || !strings.Contains(out, "move") {
			t.Errorf("issue help = %q", out)
		}
		code, out, _ = run("issue", "add", "--help")
		if code != ExitOK || !strings.Contains(out, "za issue add") {
			t.Errorf("add help = %q", out)
		}
		code, out, _ = run("board", "--help")
		if code != ExitOK || !strings.Contains(out, "za board") {
			t.Errorf("board help = %q", out)
		}
	})
}

func TestRunIssueOutsideWorkspace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := &tooltest.Fake{Effect: tooltest.Native}
	env := Env{Runner: f, Getwd: func() (string, error) { return t.TempDir(), nil }}
	code := Run(t.Context(), []string{"issue", "ls"}, &stdout, &stderr, env)
	if code != ExitFailure || !strings.Contains(stderr.String(), "not inside a za workspace") {
		t.Errorf("issue ls outside a workspace: code %d, stderr %q", code, stderr.String())
	}
	if len(f.Calls) > 0 {
		t.Errorf("native commands ran: %v", f.Calls)
	}
}
