// Package tooltest provides a deterministic tool.Runner for tests.
package tooltest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tnotstar/go-za/internal/tool"
)

// Fake records invocations instead of executing real programs.
type Fake struct {
	// Missing lists executables LookPath reports as absent.
	Missing map[string]bool
	// Effect, when non-nil, runs for every command and may simulate side
	// effects or failures. A nil Effect makes every command succeed silently.
	Effect func(tool.Command) error
	// Calls records every command passed to Run, in order.
	Calls []tool.Command
}

// LookPath implements tool.Runner.
func (f *Fake) LookPath(name string) (string, error) {
	if f.Missing[name] {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	return filepath.Join("fake", "bin", name), nil
}

// Run implements tool.Runner.
func (f *Fake) Run(_ context.Context, c tool.Command) error {
	f.Calls = append(f.Calls, c)
	if f.Effect != nil {
		return f.Effect(c)
	}
	return nil
}

// UVPyproject is the pyproject.toml content Native writes for uv init.
const UVPyproject = `[project]
name = "workspace"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = []
`

// Native simulates the files the real native initializers create.
func Native(c tool.Command) error {
	switch c.Name {
	case "git":
		return os.Mkdir(filepath.Join(c.Dir, ".git"), 0o755)
	case "go":
		return os.WriteFile(filepath.Join(c.Dir, "go.work"), []byte("go 1.24\n"), 0o644)
	case "uv":
		return os.WriteFile(filepath.Join(c.Dir, "pyproject.toml"), []byte(UVPyproject), 0o644)
	default:
		return fmt.Errorf("unexpected command: %s", c)
	}
}

// FailOn returns an Effect that simulates native behavior but fails the
// first command named name, after letting it create its usual output.
func FailOn(name string) func(tool.Command) error {
	return func(c tool.Command) error {
		if err := Native(c); err != nil {
			return err
		}
		if c.Name == name {
			return fmt.Errorf("%s: simulated failure", c)
		}
		return nil
	}
}
