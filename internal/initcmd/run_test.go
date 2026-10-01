package initcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tnotstar/go-za/internal/scaffold"
	"github.com/tnotstar/go-za/internal/tool"
	"github.com/tnotstar/go-za/internal/tool/tooltest"
)

func fakeEnv(f *tooltest.Fake) Env {
	return Env{Runner: f, Getwd: func() (string, error) { return "", errors.New("Getwd must not be called") }}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

var commonPaths = []string{
	".git",
	".gitignore",
	".gitattributes",
	".editorconfig",
	"AGENTS.md",
	"CLAUDE.md",
	"za.toml",
	".agents/context/_global/policy.md",
	".agents/context/_global/principles.md",
	".agents/context/_global/tooling.md",
	".agents/context/_global/roles.md",
	".agents/context/_global/glossary.md",
	".agents/context/_global/templates/project/INDEX.md",
	".agents/context/_global/templates/project/tools.md",
	".agents/context/_global/templates/project/constraints.md",
	".agents/context/_global/templates/project/source-of-truth.md",
	".agents/context/_global/templates/project/status.md",
	".agents/context/_global/templates/project/adr.md",
	".agents/context/projects",
	".agents/hooks/meta",
	".agents/hooks/public",
	".agents/runtime",
	".worktrees",
}

func TestRunModes(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		present []string
		absent  []string
	}{
		{
			name:    "go only",
			opts:    Options{Go: true},
			present: []string{"go.work", ".agents/context/_global/go-standards.md"},
			absent:  []string{"pyproject.toml", ".agents/context/_global/python-standards.md"},
		},
		{
			name:    "python only",
			opts:    Options{Python: true},
			present: []string{"pyproject.toml", ".agents/context/_global/python-standards.md"},
			absent:  []string{"go.work", ".agents/context/_global/go-standards.md"},
		},
		{
			name: "polyglot",
			opts: Options{Go: true, Python: true},
			present: []string{
				"go.work", "pyproject.toml",
				".agents/context/_global/go-standards.md",
				".agents/context/_global/python-standards.md",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ws")
			tt.opts.Path = root
			f := &tooltest.Fake{Effect: tooltest.Native}

			res, err := Run(t.Context(), tt.opts, fakeEnv(f))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Path != root || res.Go != tt.opts.Go || res.Python != tt.opts.Python {
				t.Errorf("Result = %+v", res)
			}
			for _, p := range append(slices.Clone(commonPaths), tt.present...) {
				if !exists(t, filepath.Join(root, filepath.FromSlash(p))) {
					t.Errorf("missing %s", p)
				}
			}
			for _, p := range tt.absent {
				if exists(t, filepath.Join(root, filepath.FromSlash(p))) {
					t.Errorf("unexpected %s", p)
				}
			}
			for _, p := range []string{"go.work.sum", "uv.lock", ".gitmodules", "openspec", ".agents/bin", ".agents/runtime/.gitkeep", ".worktrees/.gitkeep", ".agents/za.toml"} {
				if exists(t, filepath.Join(root, filepath.FromSlash(p))) {
					t.Errorf("za must not create %s", p)
				}
			}
			if got := entries(t, filepath.Join(root, ".agents/context/projects")); !slices.Equal(got, []string{".gitkeep"}) {
				t.Errorf(".agents/context/projects contains %v, want only .gitkeep", got)
			}
			manifest, err := os.ReadFile(filepath.Join(root, "za.toml"))
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("schema = 1\n\n[workspace]\ngo = %t\npython = %t\n", tt.opts.Go, tt.opts.Python)
			if string(manifest) != want {
				t.Errorf("za.toml =\n%s\nwant\n%s", manifest, want)
			}
		})
	}
}

// TestUVInitHardening guards the flags that keep uv init free of hidden
// environmental effects; dropping any of them must fail loudly.
func TestUVInitHardening(t *testing.T) {
	args := uvInit("/ws").Args
	for flag, why := range map[string]string{
		"--no-workspace":        "a parent uv workspace would capture the new root",
		"--no-python-downloads": "za init must never install a Python interpreter",
		"--no-config":           "user or system uv.toml would shape the generated files",
	} {
		if !slices.Contains(args, flag) {
			t.Errorf("uv init lacks %s: %s", flag, why)
		}
	}
}

func TestRunPreservesUnrelatedToolTables(t *testing.T) {
	root := t.TempDir()
	generated := tooltest.UVPyproject + "\n[tool.example]\nenabled = true\n"
	f := &tooltest.Fake{Effect: func(c tool.Command) error {
		if c.Name == "uv" {
			return os.WriteFile(filepath.Join(c.Dir, "pyproject.toml"), []byte(generated), 0o644)
		}
		return tooltest.Native(c)
	}}
	if _, err := Run(t.Context(), Options{Python: true, Path: root}, fakeEnv(f)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != generated+integrationTables {
		t.Errorf("pyproject.toml =\n%s\nwant\n%s", got, generated+integrationTables)
	}
}

func TestRunNativeInvocations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My Workspace")
	f := &tooltest.Fake{Effect: tooltest.Native}
	if _, err := Run(t.Context(), Options{Go: true, Python: true, Path: root}, fakeEnv(f)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []tool.Command{
		{Name: "git", Args: []string{"init", "--quiet"}, Dir: root},
		{Name: "go", Args: []string{"work", "init"}, Dir: root, Env: []string{"GOWORK=off"}},
		{Name: "uv", Args: []string{
			"init", "--bare", "--vcs", "none", "--author-from", "none", "--no-workspace",
			"--no-python-downloads", "--no-config",
			"--name", "my-workspace", root,
		}, Dir: root},
	}
	if len(f.Calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.Calls, want)
	}
	for i, c := range f.Calls {
		w := want[i]
		if c.Name != w.Name || c.Dir != w.Dir || !slices.Equal(c.Args, w.Args) || !slices.Equal(c.Env, w.Env) {
			t.Errorf("call %d = %+v, want %+v", i, c, w)
		}
		for _, shell := range []string{"sh", "bash", "cmd", "cmd.exe", "powershell"} {
			if c.Name == shell {
				t.Errorf("call %d runs through a shell: %s", i, c)
			}
		}
	}
}

func TestRunPyprojectIsIntegrationWorkspace(t *testing.T) {
	root := t.TempDir()
	f := &tooltest.Fake{Effect: tooltest.Native}
	if _, err := Run(t.Context(), Options{Python: true, Path: root}, fakeEnv(f)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, tooltest.UVPyproject) {
		t.Errorf("uv-generated [project] metadata not preserved:\n%s", got)
	}
	if !strings.HasSuffix(got, integrationTables) {
		t.Errorf("integration tables missing:\n%s", got)
	}
}

func TestRunOmittedPathUsesWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	f := &tooltest.Fake{Effect: tooltest.Native}
	env := Env{Runner: f, Getwd: func() (string, error) { return wd, nil }}

	res, err := Run(t.Context(), Options{Go: true}, env)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Path != wd {
		t.Errorf("Path = %s, want %s", res.Path, wd)
	}
	if !exists(t, filepath.Join(wd, "AGENTS.md")) {
		t.Error("working directory was not initialized")
	}
}

func TestRunRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	f := &tooltest.Fake{Effect: tooltest.Native}
	res, err := Run(t.Context(), Options{Go: true, Path: filepath.Join("a", "b")}, fakeEnv(f))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !filepath.IsAbs(res.Path) || !exists(t, filepath.Join("a", "b", "AGENTS.md")) {
		t.Errorf("relative target not initialized; Path = %s", res.Path)
	}
}

func TestRunTargetValidation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) string
		ok    bool
		want  string
	}{
		{
			name:  "nonexistent nested target is created",
			setup: func(t *testing.T, dir string) string { return filepath.Join(dir, "a", "b", "c") },
			ok:    true,
		},
		{
			name:  "existing empty target",
			setup: func(t *testing.T, dir string) string { return dir },
			ok:    true,
		},
		{
			name: "non-empty target",
			setup: func(t *testing.T, dir string) string {
				writeFile(t, filepath.Join(dir, "notes.txt"))
				return dir
			},
			want: "target directory is not empty",
		},
		{
			name: "hidden file makes target non-empty",
			setup: func(t *testing.T, dir string) string {
				writeFile(t, filepath.Join(dir, ".DS_Store"))
				return dir
			},
			want: "target directory is not empty",
		},
		{
			name: "existing za.toml is rejected, not overwritten",
			setup: func(t *testing.T, dir string) string {
				writeFile(t, filepath.Join(dir, "za.toml"))
				return dir
			},
			want: "target directory is not empty",
		},
		{
			name: "existing .git makes target non-empty",
			setup: func(t *testing.T, dir string) string {
				if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			want: "target directory is not empty",
		},
		{
			name: "regular file target",
			setup: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "file")
				writeFile(t, p)
				return p
			},
			want: "not a directory",
		},
		{
			name: "regular file ancestor",
			setup: func(t *testing.T, dir string) string {
				p := filepath.Join(dir, "file")
				writeFile(t, p)
				return filepath.Join(p, "ws")
			},
			want: "not a directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := tt.setup(t, dir)
			before := entries(t, dir)
			f := &tooltest.Fake{Effect: tooltest.Native}

			_, err := Run(t.Context(), Options{Go: true, Path: target}, fakeEnv(f))
			if tt.ok {
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if len(f.Calls) != 0 {
				t.Errorf("native tools ran before validation failed: %v", f.Calls)
			}
			if after := entries(t, dir); !slices.Equal(before, after) {
				t.Errorf("directory modified: before %v, after %v", before, after)
			}
		})
	}
}

func TestRunPreflight(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		missing string
		want    string // empty means success
	}{
		{name: "missing git", opts: Options{Go: true}, missing: "git", want: `executable "git" was not found in PATH`},
		{name: "missing go with --go", opts: Options{Go: true}, missing: "go", want: `Go workspace requested but executable "go" was not found in PATH`},
		{name: "go not required without --go", opts: Options{Python: true}, missing: "go"},
		{name: "missing uv with --python", opts: Options{Python: true}, missing: "uv", want: `Python workspace requested but executable "uv" was not found in PATH`},
		{name: "uv not required without --python", opts: Options{Go: true}, missing: "uv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "ws")
			tt.opts.Path = target
			f := &tooltest.Fake{Missing: map[string]bool{tt.missing: true}, Effect: tooltest.Native}

			_, err := Run(t.Context(), tt.opts, fakeEnv(f))
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				for _, c := range f.Calls {
					if c.Name == tt.missing {
						t.Errorf("%s was invoked although not required", tt.missing)
					}
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if len(f.Calls) != 0 {
				t.Errorf("commands ran despite failed preflight: %v", f.Calls)
			}
			if exists(t, target) {
				t.Error("target was created despite failed preflight")
			}
		})
	}
}

func TestRunCleanupOnNativeFailure(t *testing.T) {
	for _, failing := range []string{"git", "go", "uv"} {
		t.Run("new target, "+failing+" fails", func(t *testing.T) {
			parent := t.TempDir()
			writeFile(t, filepath.Join(parent, "user.txt"))
			target := filepath.Join(parent, "a", "ws")
			f := &tooltest.Fake{Effect: tooltest.FailOn(failing)}

			_, err := Run(t.Context(), Options{Go: true, Python: true, Path: target}, fakeEnv(f))
			if err == nil || !strings.Contains(err.Error(), "simulated failure") {
				t.Fatalf("error = %v, want native failure", err)
			}
			if strings.Contains(err.Error(), "cleanup incomplete") {
				t.Errorf("unexpected incomplete cleanup: %v", err)
			}
			if got := entries(t, parent); !slices.Equal(got, []string{"user.txt"}) {
				t.Errorf("parent contains %v after cleanup, want only user.txt", got)
			}
		})
	}

	t.Run("existing empty target returns to empty", func(t *testing.T) {
		target := t.TempDir()
		f := &tooltest.Fake{Effect: tooltest.FailOn("uv")}
		if _, err := Run(t.Context(), Options{Go: true, Python: true, Path: target}, fakeEnv(f)); err == nil {
			t.Fatal("expected failure")
		}
		if exists(t, target) {
			if got := entries(t, target); len(got) != 0 {
				t.Errorf("target contains %v after cleanup, want empty", got)
			}
		} else {
			t.Error("pre-existing target was removed")
		}
	})

	t.Run("unlisted native outputs are removed", func(t *testing.T) {
		target := t.TempDir()
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			if err := tooltest.Native(c); err != nil {
				return err
			}
			if c.Name == "uv" {
				// A different uv version might write extra files.
				writeFile(t, filepath.Join(c.Dir, ".python-version"))
				return errors.New("uv init: simulated failure")
			}
			return nil
		}}
		if _, err := Run(t.Context(), Options{Python: true, Path: target}, fakeEnv(f)); err == nil {
			t.Fatal("expected failure")
		}
		if got := entries(t, target); len(got) != 0 {
			t.Errorf("target contains %v after cleanup, want empty", got)
		}
	})

	t.Run("names a failed tool never created are not removed", func(t *testing.T) {
		target := t.TempDir()
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			return errors.New("git init: simulated failure before creating .git")
		}}
		in := initializer{ctx: t.Context(), runner: f, root: target}
		if err := in.run(Options{Go: true}, scaffold.Scaffold{}); err == nil {
			t.Fatal("expected failure")
		}
		if len(in.created) != 0 {
			t.Errorf("created = %v, want none: a planned but absent .git must not be scheduled for removal", in.created)
		}
	})

	t.Run("rejected uv output is cleaned up", func(t *testing.T) {
		parent := t.TempDir()
		target := filepath.Join(parent, "ws")
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			if c.Name == "uv" {
				return os.WriteFile(filepath.Join(c.Dir, "pyproject.toml"), []byte("[project]\n[tool.uv]\n"), 0o644)
			}
			return tooltest.Native(c)
		}}
		_, err := Run(t.Context(), Options{Python: true, Path: target}, fakeEnv(f))
		if err == nil || !strings.Contains(err.Error(), "defines [tool.uv]") {
			t.Fatalf("error = %v, want pyproject rejection", err)
		}
		if exists(t, target) {
			t.Error("new target was not removed")
		}
	})

	t.Run("scaffold write failure is cleaned up", func(t *testing.T) {
		target := t.TempDir()
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			if err := tooltest.Native(c); err != nil {
				return err
			}
			// Makes the scaffold's exclusive create of AGENTS.md fail midway.
			return os.Mkdir(filepath.Join(c.Dir, "AGENTS.md"), 0o755)
		}}
		_, err := Run(t.Context(), Options{Go: true, Path: target}, fakeEnv(f))
		if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
			t.Fatalf("error = %v, want AGENTS.md write failure", err)
		}
		if got := entries(t, target); len(got) != 0 {
			t.Errorf("target contains %v after cleanup, want empty", got)
		}
	})

	t.Run("failure after the manifest is written removes it", func(t *testing.T) {
		target := t.TempDir()
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			if err := tooltest.Native(c); err != nil {
				return err
			}
			// Scaffold directories are created after every file, so the
			// mkdir of .worktrees fails once za.toml already exists.
			return os.WriteFile(filepath.Join(c.Dir, ".worktrees"), nil, 0o644)
		}}
		_, err := Run(t.Context(), Options{Go: true, Path: target}, fakeEnv(f))
		if err == nil || !strings.Contains(err.Error(), "create directory .worktrees") {
			t.Fatalf("error = %v, want .worktrees creation failure", err)
		}
		if got := entries(t, target); len(got) != 0 {
			t.Errorf("target contains %v after cleanup, want empty", got)
		}
	})

	t.Run("cancelled context stops before the next step", func(t *testing.T) {
		target := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		f := &tooltest.Fake{Effect: func(c tool.Command) error {
			cancel() // interrupt arrives while git runs
			return tooltest.Native(c)
		}}
		_, err := Run(ctx, Options{Go: true, Python: true, Path: target}, fakeEnv(f))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if len(f.Calls) != 1 {
			t.Errorf("commands after cancellation: %v", f.Calls)
		}
		if got := entries(t, target); len(got) != 0 {
			t.Errorf("target contains %v after cleanup, want empty", got)
		}
	})
}

func TestProjectName(t *testing.T) {
	tests := map[string]string{
		"workspace":       "workspace",
		"My Workspace":    "my-workspace",
		"platform_v2.0":   "platform-v2-0",
		"--weird--name--": "weird-name",
		"001":             "001",
		"...":             "za-workspace",
		"日本":              "za-workspace",
	}
	for base, want := range tests {
		if got := projectName(filepath.Join("parent", base)); got != want {
			t.Errorf("projectName(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	got := Result{Path: "/w", Go: true}.Summary()
	want := "Initialized za workspace at /w\n  Git:    enabled\n  Go:     enabled\n  Python: disabled\n"
	if got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("user data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
