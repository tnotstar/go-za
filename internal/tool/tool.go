// Package tool runs native external executables (git, go, uv) directly,
// without an intermediate shell.
package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const waitDelay = 5 * time.Second

// Command describes one direct executable invocation.
type Command struct {
	Name string   // executable name, resolved through PATH
	Args []string // arguments, passed verbatim without shell interpretation
	Dir  string   // working directory
	Env  []string // KEY=VALUE entries overriding the inherited environment
}

// String renders the command for diagnostics.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Runner locates and executes native tools.
type Runner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, cmd Command) error
	// Output runs cmd like Run but returns its standard output. Standard
	// error is included in the returned error when the command fails.
	Output(ctx context.Context, cmd Command) ([]byte, error)
}

// Exec is the Runner backed by os/exec.
type Exec struct{}

// LookPath reports where name is found in PATH.
func (Exec) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// Run executes cmd and waits for it. The combined output is captured and
// included in the returned error when the command fails.
func (Exec) Run(ctx context.Context, c Command) error {
	var out bytes.Buffer
	return run(ctx, c, &out, &out)
}

// Output executes cmd and returns its standard output.
func (Exec) Output(ctx context.Context, c Command) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	if err := run(ctx, c, &stdout, &stderr); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// run executes c with the given output buffers. The content of diag is
// included in the returned error when the command fails.
func run(ctx context.Context, c Command, stdout, diag *bytes.Buffer) error {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	// Bound the wait for output pipes held open by grandchildren after the
	// command itself exits or is killed on cancellation.
	cmd.WaitDelay = waitDelay
	if len(c.Env) > 0 {
		// os/exec keeps the last value of duplicated keys, so overrides win.
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdout = stdout
	cmd.Stderr = diag

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(diag.String()); msg != "" {
			return fmt.Errorf("%s: %w\n%s", c, err, msg)
		}
		return fmt.Errorf("%s: %w", c, err)
	}
	return nil
}
