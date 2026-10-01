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
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	// Bound the wait for output pipes held open by grandchildren after the
	// command itself exits or is killed on cancellation.
	cmd.WaitDelay = waitDelay
	if len(c.Env) > 0 {
		// os/exec keeps the last value of duplicated keys, so overrides win.
		cmd.Env = append(os.Environ(), c.Env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("%s: %w\n%s", c, err, msg)
		}
		return fmt.Errorf("%s: %w", c, err)
	}
	return nil
}
