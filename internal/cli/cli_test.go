package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tnotstar/go-za/internal/initcmd"
	"github.com/tnotstar/go-za/internal/tool/tooltest"
)

func TestRun(t *testing.T) {
	nonEmpty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonEmpty, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		code       int
		stdout     string
		stderr     string
		noCommands bool
		missing    string
	}{
		{name: "init missing uv", args: []string{"init", "--python", filepath.Join(t.TempDir(), "x")}, missing: "uv", code: ExitFailure, stderr: `error: Python workspace requested but executable "uv" was not found in PATH`, noCommands: true},
		{name: "no command", args: nil, code: ExitUsage, stderr: "Usage:"},
		{name: "help", args: []string{"--help"}, code: ExitOK, stdout: "Commands:"},
		{name: "help command", args: []string{"help"}, code: ExitOK, stdout: "init"},
		{name: "unknown command", args: []string{"sync"}, code: ExitUsage, stderr: `unknown command "sync"`},
		{name: "init help", args: []string{"init", "--help"}, code: ExitOK, stdout: "za init [--go] [--python] [PATH]"},
		{name: "init without type", args: []string{"init"}, code: ExitUsage, stderr: "error: at least one workspace type is required: --go and/or --python"},
		{name: "init unknown option", args: []string{"init", "--go", "--force"}, code: ExitUsage, stderr: "flag provided but not defined", noCommands: true},
		{name: "init two paths", args: []string{"init", "--go", "a", "b"}, code: ExitUsage, stderr: "too many arguments", noCommands: true},
		{name: "init non-empty target", args: []string{"init", "--go", nonEmpty}, code: ExitFailure, stderr: "error: target directory is not empty: " + nonEmpty, noCommands: true},
		{name: "init go", args: []string{"init", "--go", filepath.Join(t.TempDir(), "go")}, code: ExitOK, stdout: "  Go:     enabled\n  Python: disabled"},
		{name: "init python", args: []string{"init", "--python", filepath.Join(t.TempDir(), "py")}, code: ExitOK, stdout: "  Go:     disabled\n  Python: enabled"},
		{name: "init polyglot", args: []string{"init", "--go", "--python", filepath.Join(t.TempDir(), "both")}, code: ExitOK, stdout: "  Go:     enabled\n  Python: enabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			f := &tooltest.Fake{Missing: map[string]bool{tt.missing: true}, Effect: tooltest.Native}
			env := initcmd.Env{Runner: f, Getwd: os.Getwd}

			code := Run(t.Context(), tt.args, &stdout, &stderr, env)
			if code != tt.code {
				t.Errorf("exit code = %d, want %d (stderr: %s)", code, tt.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.stdout) {
				t.Errorf("stdout = %q, want containing %q", stdout.String(), tt.stdout)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want containing %q", stderr.String(), tt.stderr)
			}
			if tt.code == ExitOK && stderr.Len() > 0 {
				t.Errorf("unexpected stderr on success: %q", stderr.String())
			}
			if tt.code != ExitOK && stdout.Len() > 0 {
				t.Errorf("unexpected stdout on failure: %q", stdout.String())
			}
			if tt.noCommands && len(f.Calls) > 0 {
				t.Errorf("native commands ran: %v", f.Calls)
			}
		})
	}
}
