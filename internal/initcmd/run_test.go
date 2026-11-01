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
	".agents/context/INDEX.md",
	".agents/context/policy.md",
	".agents/context/principles.md",
	".agents/context/tooling.md",
	".agents/context/roles.md",
	".agents/context/glossary.md",
	".agents/context/templates/issue.md",
	".agents/context/templates/project/INDEX.md",
	".agents/context/templates/project/tools.md",
	".agents/context/templates/project/constraints.md",
	".agents/context/templates/project/source-of-truth.md",
	".agents/context/templates/project/status.md",
	".agents/context/templates/project/adr.md",
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
			present: []string{"go.work", ".agents/context/go-standards.md"},
			absent:  []string{"pyproject.toml", ".agents/context/python-standards.md"},
		},
		{
			name:    "python only",
			opts:    Options{Python: true},
			present: []string{"pyproject.toml", ".agents/context/python-standards.md"},
			absent:  []string{"go.work", ".agents/context/go-standards.md"},
		},
		{
			name: "polyglot",
			opts: Options{Go: true, Python: true},
			present: []string{
				"go.work", "pyproject.toml",
				".agents/context/go-standards.md",
				".agents/context/python-standards.md",
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
			for _, p := range []string{"go.work.sum", "uv.lock", ".gitmodules", "openspec", ".agents/bin", ".agents/runtime/.gitkeep", ".worktrees/.gitkeep", ".agents/za.toml", ".agents/context/projects", ".agents/context/_global"} {
				if exists(t, filepath.Join(root, filepath.FromSlash(p))) {
					t.Errorf("za must not create %s", p)
				}
			}
			wantContext := []string{"INDEX.md", "policy.md", "principles.md", "roles.md", "tooling.md", "glossary.md", "templates"}
			if tt.opts.Go {
				wantContext = append(wantContext, "go-standards.md")
			}
			if tt.opts.Python {
				wantContext = append(wantContext, "python-standards.md")
			}
			if tt.opts.Rust {
				wantContext = append(wantContext, "rust-standards.md")
			}
			slices.Sort(wantContext)
			if got := entries(t, filepath.Join(root, ".agents/context")); !slices.Equal(got, wantContext) {
				t.Errorf(".agents/context contains %v, want %v", got, wantContext)
			}
			manifest, err := os.ReadFile(filepath.Join(root, "za.toml"))
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("schema = 3\n\n[workspace]\ngo = %t\npython = %t\nrust = %t\n", tt.opts.Go, tt.opts.Python, tt.opts.Rust)
			if string(manifest) != want {
				t.Errorf("za.toml =\n%s\nwant\n%s", manifest, want)
			}
		})
	}
}

// fakeRust answers the Rust probes and metadata validation of a fake run and
// simulates the native git/go/uv outputs. The metadata response is derived
// from the --manifest-path argument, so a wrongly built command fails
// validation instead of passing vacuously.
func fakeRust() *tooltest.Fake {
	return &tooltest.Fake{
		Effect: tooltest.Native,
		Respond: func(c tool.Command) ([]byte, error) {
			switch {
			case c.Name == "rustup" && slices.Contains(c.Args, "active-toolchain"):
				return []byte("stable-x86_64-unknown-linux-gnu (active, default)\n"), nil
			case c.Name == "cargo" && slices.Contains(c.Args, "--version"):
				return []byte("cargo 1.99.1 (5f94df478 2026-08-27)\n"), nil
			case c.Name == "rustc" && slices.Contains(c.Args, "--version"):
				return []byte("rustc 1.99.1 (58cf8f9c3 2026-10-13)\n"), nil
			case c.Name == "cargo" && slices.Contains(c.Args, "metadata"):
				manifest := ""
				for i, a := range c.Args {
					if a == "--manifest-path" {
						manifest = c.Args[i+1]
					}
				}
				return []byte(fmt.Sprintf(`{"version":3,"workspace_root":%q,"workspace_members":[],"packages":[],"target_directory":%q}`,
					filepath.Dir(manifest), filepath.Join(filepath.Dir(manifest), "target"))), nil
			}
			return nil, nil
		},
	}
}

// TestRunRustOnlyMode initializes a Rust-only workspace with fake tools,
// proving that neither go nor uv is probed or required.
func TestRunRustOnlyMode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	f := fakeRust()
	f.Missing = map[string]bool{"go": true, "uv": true}
	if _, err := f.LookPath("rustup"); err != nil {
		t.Fatal("rustup unexpectedly missing")
	}

	res, err := Run(t.Context(), Options{Rust: true, Path: root}, fakeEnv(f))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Rust || res.CargoLock {
		t.Errorf("Result = %+v", res)
	}
	for _, p := range append(slices.Clone(commonPaths), "Cargo.toml", ".agents/context/rust-standards.md") {
		if !exists(t, filepath.Join(root, filepath.FromSlash(p))) {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"go.work", "pyproject.toml", ".agents/context/go-standards.md",
		".agents/context/python-standards.md", "Cargo.lock", "rust-toolchain.toml", "src"} {
		if exists(t, filepath.Join(root, filepath.FromSlash(p))) {
			t.Errorf("unexpected %s", p)
		}
	}
	if got := string(readTestFile(t, filepath.Join(root, "Cargo.toml"))); got != "[workspace]\nmembers = []\nexclude = [\".worktrees\"]\nresolver = \"3\"\n" {
		t.Errorf("Cargo.toml =\n%s", got)
	}
	if gitignore := readTestFile(t, filepath.Join(root, ".gitignore")); !strings.Contains(gitignore, "/target/") || strings.Contains(gitignore, "Cargo.lock") {
		t.Errorf(".gitignore =\n%s", gitignore)
	}
	wantCalls := []tool.Command{
		{Name: "rustup", Args: []string{"show", "active-toolchain"}, Env: []string{"RUSTUP_AUTO_INSTALL=0"}},
		{Name: "cargo", Args: []string{"--version"}, Env: []string{"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TOOLCHAIN=stable-x86_64-unknown-linux-gnu"}},
		{Name: "rustc", Args: []string{"--version"}, Env: []string{"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TOOLCHAIN=stable-x86_64-unknown-linux-gnu"}},
		{Name: "git", Args: []string{"init", "--quiet"}, Dir: root},
		{Name: "cargo", Args: []string{"metadata", "--format-version", "1", "--no-deps", "--offline", "--manifest-path", filepath.Join(root, "Cargo.toml")}, Dir: root, Env: []string{"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TOOLCHAIN=stable-x86_64-unknown-linux-gnu"}},
	}
	if len(f.Calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", f.Calls, wantCalls)
	}
	for i, c := range f.Calls {
		w := wantCalls[i]
		if c.Name != w.Name || !slices.Equal(c.Args, w.Args) || !slices.Equal(c.Env, w.Env) {
			t.Errorf("call %d = %+v, want %+v", i, c, w)
		}
		for _, shell := range []string{"sh", "bash", "cmd", "cmd.exe", "powershell"} {
			if c.Name == shell {
				t.Errorf("call %d runs through a shell: %s", i, c)
			}
		}
	}
	for _, c := range f.Calls {
		if c.Name == "go" || c.Name == "uv" {
			t.Errorf("%s was invoked for a Rust-only workspace", c.Name)
		}
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

// TestRunRustPreflightFailures pins the toolchain safeguards: a missing
// override, an old toolchain or unparseable version output fails before the
// target is touched, and never runs a download-capable resolution.
func TestRunRustPreflightFailures(t *testing.T) {
	tests := []struct {
		name      string
		missing   string
		respond   func(c tool.Command) ([]byte, error)
		want      string
		wantCalls int // total calls before the failure
	}{
		{
			name: "rustup override toolchain is not installed",
			respond: func(c tool.Command) ([]byte, error) {
				if c.Name == "rustup" {
					return nil, errors.New("error: toolchain '1.70.0' is not installed")
				}
				return nil, nil
			},
			want:      "resolve the ambient override",
			wantCalls: 1,
		},
		{
			name: "cargo below the supported floor",
			respond: func(c tool.Command) ([]byte, error) {
				if c.Name == "cargo" && slices.Contains(c.Args, "--version") {
					return []byte("cargo 1.70.0 (aaaa 2024-01-01)\n"), nil
				}
				return fakeRust().Respond(c)
			},
			want:      `cargo 1.70.0 is older than the supported floor 1.85.0`,
			wantCalls: 2,
		},
		{
			name: "rustc below the supported floor",
			respond: func(c tool.Command) ([]byte, error) {
				if c.Name == "rustc" && slices.Contains(c.Args, "--version") {
					return []byte("rustc 1.84.1 (bbbb 2025-01-01)\n"), nil
				}
				return fakeRust().Respond(c)
			},
			want:      `rustc 1.84.1 is older than the supported floor 1.85.0`,
			wantCalls: 3,
		},
		{
			name: "unrecognized version output",
			respond: func(c tool.Command) ([]byte, error) {
				if c.Name == "cargo" && slices.Contains(c.Args, "--version") {
					return []byte("cargo something odd\n"), nil
				}
				return fakeRust().Respond(c)
			},
			want:      "unrecognized version",
			wantCalls: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			writeFile(t, filepath.Join(parent, "user.txt"))
			target := filepath.Join(parent, "ws")
			f := fakeRust()
			if tt.missing != "" {
				f.Missing = map[string]bool{tt.missing: true}
			}
			f.Respond = tt.respond

			_, err := Run(t.Context(), Options{Rust: true, Path: target}, fakeEnv(f))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
			if len(f.Calls) != tt.wantCalls {
				t.Errorf("calls = %d (%v), want %d", len(f.Calls), f.Calls, tt.wantCalls)
			}
			if exists(t, target) {
				t.Error("target was created despite failed preflight")
			}
			if got := entries(t, parent); !slices.Equal(got, []string{"user.txt"}) {
				t.Errorf("parent contains %v, want only user.txt", got)
			}
		})
	}

	t.Run("rustup absent probes plain executables", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "ws")
		f := fakeRust()
		f.Missing = map[string]bool{"rustup": true}

		if _, err := Run(t.Context(), Options{Rust: true, Path: target}, fakeEnv(f)); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, c := range f.Calls {
			if c.Name == "rustup" {
				t.Error("rustup was invoked although absent")
			}
			if c.Name == "cargo" || c.Name == "rustc" {
				if slices.Contains(c.Env, "RUSTUP_TOOLCHAIN=") {
					t.Errorf("%s carries a toolchain pin although rustup is absent: %+v", c.Name, c)
				}
				if !slices.Contains(c.Env, "RUSTUP_AUTO_INSTALL=0") {
					t.Errorf("%s lacks the RUSTUP_AUTO_INSTALL=0 guard: %+v", c.Name, c)
				}
			}
		}
	})

	t.Run("cargo not required without --rust", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "ws")
		f := &tooltest.Fake{Missing: map[string]bool{"cargo": true, "rustc": true, "rustup": true}, Effect: tooltest.Native}
		if _, err := Run(t.Context(), Options{Go: true, Path: target}, fakeEnv(f)); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, c := range f.Calls {
			if c.Name == "cargo" || c.Name == "rustup" || c.Name == "rustc" {
				t.Errorf("%s was invoked although Rust was not selected: %+v", c.Name, c)
			}
		}
		if exists(t, filepath.Join(target, "Cargo.toml")) {
			t.Error("Cargo.toml generated without --rust")
		}
	})
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

	t.Run("cargo metadata failure after earlier language steps cleans up", func(t *testing.T) {
		parent := t.TempDir()
		writeFile(t, filepath.Join(parent, "user.txt"))
		target := filepath.Join(parent, "a", "ws")
		f := fakeRust()
		f.Effect = tooltest.Native
		f.Respond = func(c tool.Command) ([]byte, error) {
			if c.Name == "cargo" && slices.Contains(c.Args, "metadata") {
				return nil, errors.New("cargo metadata: simulated failure")
			}
			return fakeRust().Respond(c)
		}

		_, err := Run(t.Context(), Options{Go: true, Python: true, Rust: true, Path: target}, fakeEnv(f))
		if err == nil || !strings.Contains(err.Error(), "simulated failure") {
			t.Fatalf("error = %v, want cargo metadata failure", err)
		}
		if strings.Contains(err.Error(), "cleanup incomplete") {
			t.Errorf("unexpected incomplete cleanup: %v", err)
		}
		if got := entries(t, parent); !slices.Equal(got, []string{"user.txt"}) {
			t.Errorf("parent contains %v after cleanup, want only user.txt", got)
		}
	})

	t.Run("existing empty rust target returns to empty", func(t *testing.T) {
		target := t.TempDir()
		f := fakeRust()
		f.Respond = func(c tool.Command) ([]byte, error) {
			if c.Name == "cargo" && slices.Contains(c.Args, "metadata") {
				return nil, errors.New("cargo metadata: simulated failure")
			}
			return fakeRust().Respond(c)
		}
		if _, err := Run(t.Context(), Options{Rust: true, Path: target}, fakeEnv(f)); err == nil {
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

	t.Run("cancelled rust run stops before validation and cleans up", func(t *testing.T) {
		target := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		f := fakeRust()
		f.Effect = func(c tool.Command) error {
			cancel() // interrupt arrives while git runs
			return tooltest.Native(c)
		}
		_, err := Run(ctx, Options{Rust: true, Path: target}, fakeEnv(f))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
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
	want := "Initialized za workspace at /w\n  Git:    enabled\n  Go:     enabled\n  Python: disabled\n  Rust:   disabled\n"
	if got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	rust := Result{Path: "/w", Rust: true}.Summary()
	wantRust := "Initialized za workspace at /w\n  Git:    enabled\n  Go:     disabled\n  Python: disabled\n  Rust:   enabled\n  Cargo lock: none (empty bootstrap state)\n"
	if rust != wantRust {
		t.Errorf("Summary() = %q, want %q", rust, wantRust)
	}
	locked := Result{Path: "/w", Rust: true, CargoLock: true}.Summary()
	wantLocked := "Initialized za workspace at /w\n  Git:    enabled\n  Go:     disabled\n  Python: disabled\n  Rust:   enabled\n  Cargo lock: versioned (created by Cargo)\n"
	if locked != wantLocked {
		t.Errorf("Summary() = %q, want %q", locked, wantLocked)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("user data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
