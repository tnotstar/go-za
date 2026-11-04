package initcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tnotstar/go-za/internal/tool"
)

// TestRealTools runs za init with the real native tools. It is opt-in
// (ZA_INTEGRATION=1) so the default suite never depends on uv, a Python
// interpreter, Rust tooling, or the network.
func TestRealTools(t *testing.T) {
	if testing.Short() || os.Getenv("ZA_INTEGRATION") != "1" {
		t.Skip("set ZA_INTEGRATION=1 (and omit -short) to run against real git/go/uv/cargo")
	}
	t.Setenv("UV_OFFLINE", "1")

	tests := []struct {
		name string
		opts Options
	}{
		{"go", Options{Go: true}},
		{"python", Options{Python: true}},
		{"rust", Options{Rust: true}},
		{"polyglot", Options{Go: true, Python: true}},
		{"all", Options{Go: true, Python: true, Rust: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, needed := range map[string]bool{
				"git": true, "go": tt.opts.Go, "uv": tt.opts.Python,
				"cargo": tt.opts.Rust, "rustc": tt.opts.Rust,
			} {
				if _, err := exec.LookPath(name); needed && err != nil {
					t.Skipf("%s not available", name)
				}
			}
			root := filepath.Join(t.TempDir(), "ws")
			tt.opts.Path = root
			env := Env{Runner: tool.Exec{}, Getwd: os.Getwd}
			res, err := Run(t.Context(), tt.opts, env)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "true" {
				t.Errorf("not a Git work tree: %v %s", err, out)
			}
			out, err = exec.Command("git", "-C", root, "check-ignore", ".worktrees/x", ".agents/runtime/x").CombinedOutput()
			if err != nil {
				t.Errorf("ignore rules not effective: %v %s", err, out)
			}
			if out, err := exec.Command("git", "-C", root, "check-ignore", "za.toml").CombinedOutput(); err == nil {
				t.Errorf("za.toml is ignored but must be tracked: %s", out)
			}
			manifest, err := os.ReadFile(filepath.Join(root, "za.toml"))
			if want := fmt.Sprintf("schema = 4\n\n[workspace]\ngo = %t\npython = %t\nrust = %t\n", tt.opts.Go, tt.opts.Python, tt.opts.Rust); err != nil || string(manifest) != want {
				t.Errorf("za.toml = %q (%v), want %q", manifest, err, want)
			}

			if tt.opts.Go {
				data, err := os.ReadFile(filepath.Join(root, "go.work"))
				if err != nil || !strings.HasPrefix(string(data), "go ") || strings.Contains(string(data), "use") {
					t.Errorf("unexpected go.work: %v\n%s", err, data)
				}
			}
			if tt.opts.Python {
				data, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
				if err != nil {
					t.Fatal(err)
				}
				s := string(data)
				for _, want := range []string{"[project]", `name = "ws"`, integrationTables} {
					if !strings.Contains(s, want) {
						t.Errorf("pyproject.toml lacks %q:\n%s", want, s)
					}
				}
				for _, unwanted := range []string{"README.md", ".python-version", "main.py", "uv.lock"} {
					if _, err := os.Stat(filepath.Join(root, unwanted)); err == nil {
						t.Errorf("uv created unexpected %s", unwanted)
					}
				}
				// The installed uv must accept the augmented workspace, without
				// writing a lock file or fetching anything.
				lock := exec.Command("uv", "lock", "--dry-run", "--no-config", "--no-python-downloads")
				lock.Dir = root
				if out, err := lock.CombinedOutput(); err != nil {
					t.Errorf("uv rejects the generated workspace: %v\n%s", err, out)
				}
			}
			if tt.opts.Rust {
				data, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
				if err != nil || string(data) != "[workspace]\nmembers = []\nexclude = [\".worktrees\"]\nresolver = \"3\"\n" {
					t.Errorf("unexpected Cargo.toml: %v\n%s", err, data)
				}
				for _, unwanted := range []string{"Cargo.lock", "rust-toolchain.toml", "src"} {
					if _, err := os.Stat(filepath.Join(root, unwanted)); err == nil {
						t.Errorf("za init created unexpected %s", unwanted)
					}
				}
				if !res.Rust || res.CargoLock {
					t.Errorf("Result = %+v, want Rust with no lockfile in the bootstrap state", res)
				}
				// /target/ is ignored; a root Cargo.lock is not.
				if out, err := exec.Command("git", "-C", root, "check-ignore", "target/x").CombinedOutput(); err != nil {
					t.Errorf("target/ is not ignored: %v %s", err, out)
				}
				if out, err := exec.Command("git", "-C", root, "check-ignore", "Cargo.lock").CombinedOutput(); err == nil {
					t.Errorf("Cargo.lock is ignored but must stay versioned: %s", out)
				}
				if !tt.opts.Go && !tt.opts.Python {
					for _, unwanted := range []string{"go.work", "pyproject.toml"} {
						if _, err := os.Stat(filepath.Join(root, unwanted)); err == nil {
							t.Errorf("rust-only init created unexpected %s", unwanted)
						}
					}
				}
			} else {
				if _, err := os.Stat(filepath.Join(root, "Cargo.toml")); err == nil {
					t.Error("Cargo.toml generated without --rust")
				}
				if data, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil && strings.Contains(string(data), "/target/") {
					t.Error(".gitignore gained the /target/ rule without --rust")
				}
			}
		})
	}
}

// cargoMeta is the subset of cargo metadata output the boundary fixtures
// assert on. Unknown fields are ignored.
type cargoMeta struct {
	WorkspaceRoot    string            `json:"workspace_root"`
	WorkspaceMembers []string          `json:"workspace_members"`
	TargetDirectory  string            `json:"target_directory"`
	Packages         []json.RawMessage `json:"packages"`
}

// runCargoMetadata runs real Cargo for the boundary fixtures. An empty manifest
// argument resolves from dir; RUSTUP_AUTO_INSTALL=0 keeps rustup from
// implicitly installing anything.
func runCargoMetadata(t *testing.T, dir, manifest string) cargoMeta {
	t.Helper()
	args := []string{"metadata", "--format-version", "1", "--no-deps", "--offline"}
	if manifest != "" {
		args = append(args, "--manifest-path", manifest)
	}
	cmd := exec.Command("cargo", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "RUSTUP_AUTO_INSTALL=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cargo metadata (dir %s, manifest %q): %v\n%s", dir, manifest, err, out)
	}
	var m cargoMeta
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("parse cargo metadata: %v\n%s", err, out)
	}
	return m
}

func memberID(m cargoMeta, name string) bool {
	for _, id := range m.WorkspaceMembers {
		if strings.Contains(id, "/"+name+"#") {
			return true
		}
	}
	return false
}

// writeCrate creates a minimal real crate: a manifest plus src/lib.rs, the
// target Cargo requires before it accepts a package manifest.
func writeCrate(t *testing.T, dir, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "lib.rs"), []byte("pub fn hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func crateManifest(name string) string {
	return fmt.Sprintf("[package]\nname = %q\nversion = \"0.1.0\"\nedition = \"2021\"\n", name)
}

// TestRealCargoBoundaries converts the reviewed Cargo experiments into
// reproducible boundary checks on a real, za-initialized workspace:
// empty-workspace metadata, effective roots, worktree exclusion, nesting
// versus membership, and ancestor configuration discovery. All fixtures are
// local; nothing touches a registry or the network.
func TestRealCargoBoundaries(t *testing.T) {
	if testing.Short() || os.Getenv("ZA_INTEGRATION") != "1" {
		t.Skip("set ZA_INTEGRATION=1 (and omit -short) to run the real Cargo boundaries")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skipf("cargo not available")
	}
	root := filepath.Join(t.TempDir(), "meta")
	if _, err := Run(t.Context(), Options{Rust: true, Path: root}, Env{Runner: tool.Exec{}, Getwd: os.Getwd}); err != nil {
		t.Fatalf("za init --rust: %v", err)
	}

	t.Run("empty initialized virtual workspace", func(t *testing.T) {
		m := runCargoMetadata(t, root, filepath.Join(root, "Cargo.toml"))
		if m.WorkspaceRoot != root {
			t.Errorf("workspace_root = %q, want %q", m.WorkspaceRoot, root)
		}
		if len(m.WorkspaceMembers) != 0 || len(m.Packages) != 0 {
			t.Errorf("members = %v, packages = %d, want empty", m.WorkspaceMembers, len(m.Packages))
		}
		if _, err := os.Stat(filepath.Join(root, "Cargo.lock")); err == nil {
			t.Error("cargo metadata created a root Cargo.lock; the empty bootstrap state must stay lockless unless Cargo really creates one")
		}
	})

	t.Run("member manifest selects the parent workspace", func(t *testing.T) {
		writeCrate(t, filepath.Join(root, "crates", "alpha"), crateManifest("alpha"))
		rootManifest := filepath.Join(root, "Cargo.toml")
		data, err := os.ReadFile(rootManifest)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(data), "members = []", `members = ["crates/alpha"]`, 1)
		if err := os.WriteFile(rootManifest, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(rootManifest, data, 0o644)

		m := runCargoMetadata(t, root, filepath.Join(root, "crates", "alpha", "Cargo.toml"))
		if m.WorkspaceRoot != root {
			t.Errorf("workspace_root = %q, want the parent %q: a member manifest must not pose as standalone", m.WorkspaceRoot, root)
		}
		if !memberID(m, "alpha") {
			t.Errorf("members = %v, want alpha", m.WorkspaceMembers)
		}
	})

	t.Run("task crate under .worktrees is excluded and its own root", func(t *testing.T) {
		task := filepath.Join(root, ".worktrees", "foo", "task1")
		writeCrate(t, task, crateManifest("task1"))

		m := runCargoMetadata(t, root, filepath.Join(task, "Cargo.toml"))
		if m.WorkspaceRoot != task {
			t.Errorf("workspace_root = %q, want the task crate %q", m.WorkspaceRoot, task)
		}
		if len(m.WorkspaceMembers) != 1 || !memberID(m, "task1") {
			t.Errorf("members = %v, want only task1", m.WorkspaceMembers)
		}
		if from := runCargoMetadata(t, task, ""); from.WorkspaceRoot != task {
			t.Errorf("workspace_root from inside the task = %q, want %q", from.WorkspaceRoot, task)
		}
	})

	t.Run("independent task workspace under .worktrees keeps its members", func(t *testing.T) {
		task2 := filepath.Join(root, ".worktrees", "bar", "task2")
		writeCrate(t, filepath.Join(task2, "beta"), crateManifest("beta"))
		writeCrate(t, task2, "[workspace]\nmembers = [\"beta\"]\nresolver = \"2\"\n")

		m := runCargoMetadata(t, root, filepath.Join(task2, "Cargo.toml"))
		if m.WorkspaceRoot != task2 {
			t.Errorf("workspace_root = %q, want %q", m.WorkspaceRoot, task2)
		}
		if !memberID(m, "beta") {
			t.Errorf("members = %v, want beta", m.WorkspaceMembers)
		}
	})

	t.Run("nested independent workspace is not a member", func(t *testing.T) {
		nested := filepath.Join(root, "nested")
		writeCrate(t, filepath.Join(nested, "gamma"), crateManifest("gamma"))
		writeCrate(t, nested, "[workspace]\nmembers = [\"gamma\"]\nresolver = \"2\"\n")

		m := runCargoMetadata(t, root, filepath.Join(nested, "Cargo.toml"))
		if m.WorkspaceRoot != nested {
			t.Errorf("workspace_root = %q, want %q: nesting is not membership", m.WorkspaceRoot, nested)
		}
		if !memberID(m, "gamma") {
			t.Errorf("members = %v, want gamma", m.WorkspaceMembers)
		}
	})

	t.Run("separate checkout outside the meta root resolves independently", func(t *testing.T) {
		outside := filepath.Join(filepath.Dir(root), "outside")
		writeCrate(t, filepath.Join(outside, "delta"), crateManifest("delta"))
		writeCrate(t, outside, "[workspace]\nmembers = [\"delta\"]\nresolver = \"2\"\n")

		m := runCargoMetadata(t, outside, "")
		if m.WorkspaceRoot != outside {
			t.Errorf("workspace_root = %q, want %q", m.WorkspaceRoot, outside)
		}
	})

	t.Run("stray package at the meta root fails without membership", func(t *testing.T) {
		stray := filepath.Join(root, "stray")
		writeCrate(t, stray, crateManifest("stray"))
		defer os.RemoveAll(stray)

		cmd := exec.Command("cargo", "metadata", "--format-version", "1", "--no-deps", "--offline",
			"--manifest-path", filepath.Join(stray, "Cargo.toml"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "RUSTUP_AUTO_INSTALL=0")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("stray non-member package resolved; want a workspace error:\n%s", out)
		} else if !strings.Contains(string(out), "believes it's in a workspace") {
			t.Errorf("unexpected error for stray package: %v\n%s", err, out)
		}
	})

	t.Run("ancestor configuration is discovered despite exclusion", func(t *testing.T) {
		cfgDir := filepath.Join(root, ".cargo")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := filepath.Join(cfgDir, "config.toml")
		if err := os.WriteFile(cfg, []byte("[build]\ntarget-dir = \"integration-target\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(cfg)

		task := filepath.Join(root, ".worktrees", "foo", "task1")
		m := runCargoMetadata(t, task, "")
		if want := filepath.Join(root, "integration-target"); m.TargetDirectory != want {
			t.Errorf("target_directory = %q, want %q: worktree exclusion must not isolate ancestor Cargo configuration", m.TargetDirectory, want)
		}
		if from := runCargoMetadata(t, root, filepath.Join(root, "Cargo.toml")); from.TargetDirectory != filepath.Join(root, "integration-target") {
			t.Errorf("target_directory from the meta root = %q", from.TargetDirectory)
		}
	})
}
