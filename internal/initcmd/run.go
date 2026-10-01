package initcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tnotstar/go-za/internal/scaffold"
	"github.com/tnotstar/go-za/internal/tool"
	"github.com/tnotstar/go-za/internal/workspace"
)

// Env holds the process-level dependencies of Run.
type Env struct {
	Runner tool.Runner
	Getwd  func() (string, error)
}

// Result describes a successful initialization.
type Result struct {
	Path   string
	Go     bool
	Python bool
}

// Summary renders the script-friendly success report.
func (r Result) Summary() string {
	return fmt.Sprintf("Initialized za workspace at %s\n  Git:    enabled\n  Go:     %s\n  Python: %s\n",
		r.Path, enabled(r.Go), enabled(r.Python))
}

func enabled(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

// Run initializes a meta-workspace. All validation happens before the first
// mutation; if initialization fails afterwards, Run restores the target on a
// best-effort basis without touching entries it did not create.
func Run(ctx context.Context, opts Options, env Env) (Result, error) {
	if !opts.Go && !opts.Python {
		return Result{}, &UsageError{"at least one workspace type is required: --go and/or --python"}
	}

	path := opts.Path
	if path == "" {
		wd, err := env.Getwd()
		if err != nil {
			return Result{}, fmt.Errorf("determine current directory: %w", err)
		}
		path = wd
	}
	target, err := workspace.Inspect(path)
	if err != nil {
		return Result{}, err
	}
	if err := preflight(env.Runner, opts); err != nil {
		return Result{}, err
	}
	files, err := scaffold.Render(scaffold.Options{Go: opts.Go, Python: opts.Python})
	if err != nil {
		return Result{}, err
	}

	if err := target.Create(); err != nil {
		return Result{}, err
	}
	in := initializer{ctx: ctx, runner: env.Runner, root: target.Path}
	if err := in.run(opts, files); err != nil {
		if cerr := target.Cleanup(in.created); cerr != nil {
			return Result{}, fmt.Errorf("%w\nwarning: cleanup incomplete: %v", err, cerr)
		}
		return Result{}, err
	}
	return Result{Path: target.Path, Go: opts.Go, Python: opts.Python}, nil
}

// preflight verifies every required executable before any mutation.
func preflight(r tool.Runner, opts Options) error {
	required := []struct {
		name, purpose string
		needed        bool
	}{
		{"git", "Git is always required", true},
		{"go", "Go workspace requested", opts.Go},
		{"uv", "Python workspace requested", opts.Python},
	}
	var errs []error
	for _, req := range required {
		if !req.needed {
			continue
		}
		if _, err := r.LookPath(req.name); err != nil {
			errs = append(errs, fmt.Errorf("%s but executable %q was not found in PATH", req.purpose, req.name))
		}
	}
	return errors.Join(errs...)
}

type initializer struct {
	ctx    context.Context
	runner tool.Runner
	root   string
	// created lists the top-level entries that appeared in the target while
	// a step of this run executed; only these are removed on failure.
	created []string
}

func (in *initializer) run(opts Options, files scaffold.Scaffold) error {
	if err := in.native(tool.Command{
		Name: "git", Args: []string{"init", "--quiet"}, Dir: in.root,
	}); err != nil {
		return err
	}

	if opts.Go {
		// GOWORK=off keeps a go.work in a parent directory from affecting init.
		if err := in.native(tool.Command{
			Name: "go", Args: []string{"work", "init"}, Dir: in.root, Env: []string{"GOWORK=off"},
		}); err != nil {
			return err
		}
	}

	if opts.Python {
		if err := in.native(uvInit(in.root)); err != nil {
			return err
		}
		if err := in.step(func() error {
			return makeIntegrationWorkspace(filepath.Join(in.root, "pyproject.toml"))
		}); err != nil {
			return err
		}
	}

	return in.step(func() error { return files.Write(in.root) })
}

// native runs a native initializer as one step.
func (in *initializer) native(cmd tool.Command) error {
	return in.step(func() error {
		if err := in.runner.Run(in.ctx, cmd); err != nil {
			return fmt.Errorf("native initialization failed: %w", err)
		}
		return nil
	})
}

// step runs fn unless the run was cancelled, and records every top-level
// entry that appeared in the target meanwhile, whether fn succeeded or not.
func (in *initializer) step(fn func() error) error {
	if err := in.ctx.Err(); err != nil {
		return fmt.Errorf("initialization interrupted: %w", err)
	}
	before, err := entryNames(in.root)
	if err != nil {
		return err
	}
	stepErr := fn()
	after, err := entryNames(in.root)
	for name := range after {
		if !before[name] {
			in.created = append(in.created, name)
		}
	}
	return errors.Join(stepErr, err)
}

func entryNames(dir string) (map[string]bool, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list target %s: %w", dir, err)
	}
	names := make(map[string]bool, len(des))
	for _, de := range des {
		names[de.Name()] = true
	}
	return names, nil
}

// uvInit creates only a bare pyproject.toml: no Git repository, no inferred
// author, and no membership in a parent uv workspace.
func uvInit(root string) tool.Command {
	return tool.Command{
		Name: "uv",
		Args: []string{
			"init", "--bare",
			"--vcs", "none",
			"--author-from", "none",
			"--no-workspace",
			"--name", projectName(root),
			root,
		},
		Dir: root,
	}
}

// projectName derives a valid, normalized (PEP 503) Python project name from
// the target directory, so that directory names uv would reject still work.
func projectName(root string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(filepath.Base(root)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingDash {
				b.WriteByte('-')
				pendingDash = false
			}
			b.WriteRune(r)
		} else if b.Len() > 0 {
			pendingDash = true
		}
	}
	if b.Len() == 0 {
		return "za-workspace"
	}
	return b.String()
}
