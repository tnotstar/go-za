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

// wikiWorkspace builds a minimal valid za workspace with a wiki directory,
// a wiki template and one registered project.
func wikiWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"za.toml":                     "schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n",
		".gitmodules":                 "[submodule \"go-za\"]\n\tpath = go-za\n\turl = https://example.invalid/go-za.git\n",
		".za/templates/wiki.md":       "# <Page Title>\n\nSummary: <one line.>\n\nStatus: <draft | accepted>\n",
		".za/wiki/index.md":           "# Workspace Wiki Index\n\nSummary: Navigation root.\n",
		".za/wiki/go-minolas-arch.md": "# go-minolas: Architecture\n\nPackage graph.\n",
		".za/wiki/img/note.txt":       "asset, not a page",
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

func TestRunWikiCommands(t *testing.T) {
	root := wikiWorkspace(t)
	inRoot := func() (string, error) { return root, nil }

	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		f := &tooltest.Fake{Effect: tooltest.Native}
		env := Env{Runner: f, Getwd: inRoot, Now: func() time.Time { return time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC) }}
		code := Run(t.Context(), args, &stdout, &stderr, env)
		return code, stdout.String(), stderr.String()
	}

	t.Run("ls is deterministic and skips non-pages", func(t *testing.T) {
		code, out, errOut := run("wiki", "ls")
		if code != ExitOK || errOut != "" {
			t.Fatalf("wiki ls: code %d, stderr %q", code, errOut)
		}
		want := "go-minolas-arch\t.za/wiki/go-minolas-arch.md\tgo-minolas: Architecture\n" +
			"index\t.za/wiki/index.md\tWorkspace Wiki Index\n"
		if out != want {
			t.Errorf("wiki ls =\n%s\nwant\n%s", out, want)
		}
	})

	t.Run("new uses the template heading and prints the path", func(t *testing.T) {
		code, out, errOut := run("wiki", "new", "-title", "A Clean Title", "spec-page")
		if code != ExitOK || errOut != "" {
			t.Fatalf("wiki new: code %d, stderr %q", code, errOut)
		}
		if out != ".za/wiki/spec-page.md\n" {
			t.Errorf("wiki new output = %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".za", "wiki", "spec-page.md"))
		if !strings.HasPrefix(string(data), "# A Clean Title\n\nSummary: <one line.>") {
			t.Errorf("page = %q", data)
		}
	})

	t.Run("new derives the title from the slug", func(t *testing.T) {
		code, out, errOut := run("wiki", "new", "go-za-design")
		if code != ExitOK || errOut != "" {
			t.Fatalf("wiki new: code %d, stderr %q", code, errOut)
		}
		if out != ".za/wiki/go-za-design.md\n" {
			t.Errorf("wiki new output = %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".za", "wiki", "go-za-design.md"))
		if !strings.HasPrefix(string(data), "# Go Za Design\n\nSummary: <one line.>") {
			t.Errorf("page = %q", data)
		}
	})

	t.Run("new never overwrites and rejects bad slugs", func(t *testing.T) {
		code, _, errOut := run("wiki", "new", "index")
		if code != ExitFailure || !strings.Contains(errOut, "create .za/wiki/index.md") {
			t.Errorf("wiki new existing: code %d, stderr %q", code, errOut)
		}
		code, _, errOut = run("wiki", "new", "img")
		if code != ExitFailure || !strings.Contains(errOut, "reserved") {
			t.Errorf("wiki new img: code %d, stderr %q", code, errOut)
		}
		code, _, errOut = run("wiki", "new", "../escape")
		if code != ExitFailure || !strings.Contains(errOut, "path separator") {
			t.Errorf("wiki new traversal: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("show accepts the slug with or without .md", func(t *testing.T) {
		code, out, errOut := run("wiki", "show", "go-minolas-arch")
		if code != ExitOK || errOut != "" || !strings.HasPrefix(out, "# go-minolas: Architecture\n") {
			t.Errorf("wiki show = code %d, out %q, stderr %q", code, out, errOut)
		}
		code, out2, _ := run("wiki", "show", "go-minolas-arch.md")
		if code != ExitOK || out2 != out {
			t.Errorf("wiki show with .md differs")
		}
		code, _, errOut = run("wiki", "show", "missing")
		if code != ExitFailure || !strings.Contains(errOut, "does not exist") {
			t.Errorf("wiki show missing: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("usage errors exit with code 2", func(t *testing.T) {
		for _, args := range [][]string{
			{"wiki"},
			{"wiki", "frobnicate"},
			{"wiki", "new"},
			{"wiki", "new", "a", "b"},
			{"wiki", "show"},
			{"wiki", "show", "a", "b"},
			{"wiki", "ls", "extra"},
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
		code, out, _ := run("wiki", "--help")
		if code != ExitOK || !strings.Contains(out, "Commands:") || !strings.Contains(out, "show") {
			t.Errorf("wiki help = %q", out)
		}
		code, out, _ = run("wiki", "new", "--help")
		if code != ExitOK || !strings.Contains(out, "za wiki new") {
			t.Errorf("wiki new help = %q", out)
		}
	})
}

func TestRunWikiOutsideWorkspace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := &tooltest.Fake{Effect: tooltest.Native}
	env := Env{Runner: f, Getwd: func() (string, error) { return t.TempDir(), nil }}
	code := Run(t.Context(), []string{"wiki", "ls"}, &stdout, &stderr, env)
	if code != ExitFailure || !strings.Contains(stderr.String(), "not inside a za workspace") {
		t.Errorf("wiki ls outside a workspace: code %d, stderr %q", code, stderr.String())
	}
	if len(f.Calls) > 0 {
		t.Errorf("native commands ran: %v", f.Calls)
	}
}
