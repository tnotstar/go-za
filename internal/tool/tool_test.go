package tool

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// These tests use the go executable, which is always present when the Go
// test suite runs.

func TestExecRunSuccess(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	err := Exec{}.Run(t.Context(), Command{Name: "go", Args: []string{"env", "GOWORK"}, Dir: t.TempDir(), Env: []string{"GOWORK=off"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestExecRunFailureIncludesOutput(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	err := Exec{}.Run(t.Context(), Command{Name: "go", Args: []string{"no-such-subcommand"}, Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "go no-such-subcommand") || !strings.Contains(msg, "no-such-subcommand") {
		t.Errorf("error lacks command context: %q", msg)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("underlying *exec.ExitError not preserved: %v", err)
	}
}

func TestExecLookPathMissing(t *testing.T) {
	if _, err := (Exec{}).LookPath("za-definitely-missing-executable"); err == nil {
		t.Error("expected LookPath failure")
	}
}

func TestCommandString(t *testing.T) {
	c := Command{Name: "uv", Args: []string{"init", "--bare"}}
	if got := c.String(); got != "uv init --bare" {
		t.Errorf("String() = %q", got)
	}
}
