package issue

import (
	"fmt"
	"time"
)

// Env holds the process-level dependencies of the issue and board
// commands. Getwd is the working-directory probe (always provided by the
// CLI, mirroring the other subcommands); the commands perform pure file
// operations on the workspace, so they need no native command runner.
type Env struct {
	Getwd func() (string, error)
	Now   func() time.Time
}

// Root returns the workspace root enclosing the process working directory.
func (e Env) Root() (string, error) {
	wd, err := e.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine current directory: %w", err)
	}
	return Root(wd)
}

// now returns the process clock, defaulting to the wall clock when the CLI
// provided none.
func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
