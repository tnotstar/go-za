// Real-Git onboarding flows for Rust projects. Cargo itself is faked by
// fakeCargo so the default suite needs no Rust toolchain; the real-cargo
// contract is covered separately by the opt-in TestAddRealCargoRust.
package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tnotstar/go-za/internal/initcmd"
	"github.com/tnotstar/go-za/internal/scaffold"
	"github.com/tnotstar/go-za/internal/tool"
)

// fakeCargo delegates git (and go) to the real executor while answering
// every cargo and rustc invocation deterministically: rustup is absent, the
// version probes satisfy the floor, and "cargo metadata" returns a response
// anchored at the invocation directory.
type fakeCargo struct {
	recorder
	failMeta bool
}

func (f *fakeCargo) LookPath(name string) (string, error) {
	if name == "rustup" {
		return "", exec.ErrNotFound
	}
	return f.recorder.LookPath(name)
}

func (f *fakeCargo) Run(ctx context.Context, c tool.Command) error {
	_, err := f.Output(ctx, c)
	return err
}

func (f *fakeCargo) Output(ctx context.Context, c tool.Command) ([]byte, error) {
	switch {
	case c.Name == "cargo" && slices.Contains(c.Args, "metadata"):
		f.calls = append(f.calls, c)
		if f.fail != nil {
			if err := f.fail(c); err != nil {
				return nil, err
			}
		}
		if f.failMeta {
			return nil, errors.New("error: simulated cargo failure")
		}
		return []byte(fmt.Sprintf(`{"workspace_root":%q,"workspace_members":["%s"],"packages":[{"name":"pub"}]}`,
			c.Dir, filepath.Join(c.Dir, "pub"))), nil
	case c.Name == "cargo" && slices.Contains(c.Args, "--version"):
		return []byte("cargo 1.99.1 (0000000 2026-01-01)\n"), nil
	case c.Name == "rustc" && slices.Contains(c.Args, "--version"):
		return []byte("rustc 1.99.0 (0000000 2026-01-01)\n"), nil
	default:
		return f.recorder.Output(ctx, c)
	}
}

// rustWorkspace scaffolds a Rust-only workspace with the real generator and
// commits the bootstrap, without running za init's cargo validation.
func rustWorkspace(t *testing.T, commit bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	files, err := scaffold.Render(scaffold.Options{Rust: true})
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if err := files.Write(root); err != nil {
		t.Fatalf("scaffold write: %v", err)
	}
	if commit {
		git(t, root, "init", "--quiet")
		git(t, root, "add", "--all")
		git(t, root, "commit", "--quiet", "-m", "bootstrap")
	}
	return root
}

var rustRepo = map[string]string{
	"Cargo.toml":  "[package]\nname = \"pub\"\nversion = \"0.1.0\"\nedition = \"2021\"\n",
	"src/main.rs": "fn main() {}\n",
}

// xesubstRepo is the nested-workspace shape observed in the wild: a virtual
// root manifest with crates below it.
var xesubstRepo = map[string]string{
	"Cargo.toml":            "[workspace]\nmembers = [\"crates/pub\", \"crates/xes\"]\nresolver = \"2\"\n",
	"crates/pub/Cargo.toml": "[package]\nname = \"pub\"\nversion = \"0.1.0\"\n",
	"crates/xes/Cargo.toml": "[package]\nname = \"xes\"\nversion = \"0.1.0\"\n",
}

func withRustOpenSpec(files map[string]string) map[string]string {
	out := map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/specs/.gitkeep": ""}
	for k, v := range files {
		out[k] = v
	}
	return out
}

func withCargoLock(files map[string]string) map[string]string {
	out := map[string]string{"Cargo.lock": "# lock\nversion = 4\n"}
	for k, v := range files {
		out[k] = v
	}
	return out
}

func TestAddRealGitRust(t *testing.T) {
	hermetic(t)
	for _, variant := range []string{"openspec-absent", "openspec-present", "with-lock"} {
		t.Run(variant, func(t *testing.T) {
			files := rustRepo
			switch variant {
			case "openspec-present":
				files = withRustOpenSpec(rustRepo)
			case "with-lock":
				files = withCargoLock(rustRepo)
			}
			pub := publicRepo(t, "pub", files)
			pubHead := strings.TrimSpace(git(t, pub, "rev-parse", "HEAD"))
			root := rustWorkspace(t, true)
			before := capture(t, root)
			fc := &fakeCargo{}

			res, err := Add(t.Context(), pub, realEnv(fc, root))
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			if res.ID != "pub" || res.Gitlink != pubHead || res.Language != "rust" || res.Crate != "pub" || res.Edition != "2021" || res.OpenSpec != (variant == "openspec-present") {
				t.Errorf("unexpected result %+v (public HEAD %s)", res, pubHead)
			}
			summary := res.Summary()
			for _, want := range []string{
				`Added project "pub"`, "Project:    pub/",
				"Rust crate:  pub (added to /Cargo.toml members; edition 2021)",
				"Pinned gitlink: " + pubHead, "context verification pending", "staged but not committed",
			} {
				if !strings.Contains(summary, want) {
					t.Errorf("summary lacks %q:\n%s", want, summary)
				}
			}

			// The cargo metadata call is offline, no-deps, anchored at the
			// workspace root, and pinned hermetically; nothing else ran.
			metas := 0
			for _, c := range fc.calls {
				switch c.Name {
				case "cargo":
					if !slices.Contains(c.Args, "metadata") || !slices.Contains(c.Args, "--no-deps") || !slices.Contains(c.Args, "--offline") {
						t.Errorf("cargo invoked without the offline no-deps contract: %v", c)
					}
					if c.Dir != root {
						t.Errorf("cargo metadata ran in %q, want the workspace root", c.Dir)
					}
					if !slices.Contains(c.Env, "RUSTUP_AUTO_INSTALL=0") {
						t.Errorf("cargo metadata lacks the auto-install guard: %v", c)
					}
					metas++
				case "git":
				default:
					t.Errorf("unexpected executable %q", c.Name)
				}
			}
			if metas != 1 {
				t.Errorf("cargo metadata calls = %d, want 1", metas)
			}

			// Root manifest: exactly the new member, comments intact.
			manifest := readFile(t, filepath.Join(root, "Cargo.toml"))
			if !strings.Contains(manifest, "members = [\"pub\"]") {
				t.Errorf("Cargo.toml members not updated:\n%s", manifest)
			}
			if !strings.Contains(manifest, "exclude = [\".worktrees\"]") || !strings.Contains(manifest, "resolver = \"3\"") {
				t.Errorf("Cargo.toml lost scaffold keys:\n%s", manifest)
			}
			if present(t, filepath.Join(root, "Cargo.lock")) {
				t.Error("a root Cargo.lock was created; za never writes lockfiles")
			}

			// Staged change: Cargo.toml replaces go.work in the Go layout.
			staged := strings.Fields(git(t, root, "diff", "--cached", "--name-only"))
			wantStaged := []string{
				".agents/context/pub/AGENTS.md",
				".agents/context/pub/adr.md",
				".agents/context/pub/decisions/.gitkeep",
				".agents/context/pub/sot.md",
				".agents/context/pub/status.log",
				".agents/context/pub/status.md",
				".agents/context/pub/tools.md",
				".gitmodules", "AGENTS.md", "Cargo.toml", "pub",
			}
			if !slices.Equal(staged, wantStaged) {
				t.Errorf("staged = %v, want %v", staged, wantStaged)
			}
			if head := git(t, root, "rev-parse", "HEAD"); head != before.head {
				t.Error("a commit was created")
			}
			if out := git(t, root, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none"); strings.ContainsAny(strings.Join(lineYs(out), ""), "MD?") {
				t.Errorf("unstaged or untracked changes remain:\n%s", out)
			}
			if got := readFile(t, filepath.Join(root, "za.toml")); got != before.files["za.toml"] {
				t.Errorf("za.toml modified:\n%s", got)
			}

			// Private context against golden files, gitlink masked.
			ctxDir := filepath.Join(root, ".agents", "context", "pub")
			for _, name := range contextFiles {
				got := strings.ReplaceAll(readFile(t, filepath.Join(ctxDir, name)), pubHead, "<GITLINK>")
				golden := filepath.Join("testdata", "golden", "rust-"+variant, name+".golden")
				if *update {
					writeFile(t, golden, got)
					continue
				}
				if want := readFile(t, golden); got != want {
					t.Errorf("%s differs from %s (run with -update):\n%s", name, golden, got)
				}
			}
			tools := readFile(t, filepath.Join(ctxDir, "tools.md"))
			if !strings.Contains(tools, "| Rust | yes | version source `pub/Cargo.toml` (edition 2021); <discover: invocation> |") {
				t.Errorf("tools.md lacks the Rust capability row:\n%s", tools)
			}
			if !strings.Contains(tools, "| Go | no | not applicable |") {
				t.Errorf("tools.md lacks the Go not-applicable row:\n%s", tools)
			}
			sot := readFile(t, filepath.Join(ctxDir, "sot.md"))
			if !strings.Contains(sot, "| Cargo dependencies | `pub` repository | `pub/Cargo.toml` |") {
				t.Errorf("sot.md lacks the Cargo dependencies row:\n%s", sot)
			}
			wantLock := "| Cargo dependencies | `pub` repository | `pub/Cargo.toml` | not applicable |"
			if variant == "with-lock" {
				wantLock = "| Cargo dependencies | `pub` repository | `pub/Cargo.toml` | `pub/Cargo.lock` |"
			}
			if !strings.Contains(sot, wantLock) {
				t.Errorf("sot.md Cargo lock evidence = want %q:\n%s", wantLock, sot)
			}
		})
	}
}

func TestAddRealGitRustFailures(t *testing.T) {
	hermetic(t)
	tests := []struct {
		name      string
		files     map[string]string
		workspace func(t *testing.T) string // nil: the default Go workspace
		err       string
	}{
		{
			name:      "nested virtual workspace",
			files:     xesubstRepo,
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       `project "pub" declares a [workspace] table in its root Cargo.toml`,
		},
		{
			name: "composite package and workspace",
			files: map[string]string{
				"Cargo.toml":  "[package]\nname = \"pub\"\n\n[workspace]\nmembers = []\n",
				"src/main.rs": "fn main() {}\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "declares a [workspace] table in its root Cargo.toml",
		},
		{
			name: "no package name",
			files: map[string]string{
				"Cargo.toml":  "[package]\nversion = \"0.1.0\"\n",
				"src/main.rs": "fn main() {}\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "no [package] table with a package.name",
		},
		{
			name: "hostile crate name",
			files: map[string]string{
				"Cargo.toml":  "[package]\nname = \"a | b\\n\"\nversion = \"0.1.0\"\n",
				"src/main.rs": "fn main() {}\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "unsupported crate name",
		},
		{
			name: "hostile edition",
			files: map[string]string{
				"Cargo.toml":  "[package]\nname = \"pub\"\nversion = \"0.1.0\"\nedition = \"2021 | no | x\"\n",
				"src/main.rs": "fn main() {}\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "unsupported edition",
		},
		{
			name: "nested crate manifest",
			files: map[string]string{
				"Cargo.toml":              "[package]\nname = \"pub\"\nversion = \"0.1.0\"\n",
				"src/main.rs":             "fn main() {}\n",
				"crates/extra/Cargo.toml": "[package]\nname = \"extra\"\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "nested Cargo.toml manifests",
		},
		{
			name: "invalid manifest",
			files: map[string]string{
				"Cargo.toml": "[package\n",
			},
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "has an invalid Cargo.toml",
		},
		{
			name:      "rust project in a Go-only workspace",
			files:     rustRepo,
			workspace: nil,
			err:       "the workspace is not Rust-enabled",
		},
		{
			name:      "go project in a Rust-only workspace",
			files:     goRepo,
			workspace: func(t *testing.T) string { return rustWorkspace(t, true) },
			err:       "the workspace is not Go-enabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := publicRepo(t, "pub", tt.files)
			root := workspace(t, true)
			if tt.workspace != nil {
				root = tt.workspace(t)
			}
			before := capture(t, root)

			_, err := Add(t.Context(), pub, realEnv(&fakeCargo{}, root))
			if err == nil || !strings.Contains(err.Error(), tt.err) || strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("err = %v, want containing %q", err, tt.err)
			}
			assertRestored(t, root, "pub", before)
		})
	}
}

func TestAddRealGitRustRollback(t *testing.T) {
	hermetic(t)
	steps := []string{"submodule", "validate", "members", "context", "routing", "stage"}
	for _, step := range steps {
		t.Run("after "+step, func(t *testing.T) {
			pub := publicRepo(t, "pub", rustRepo)
			root := rustWorkspace(t, true)
			before := capture(t, root)
			env := realEnv(&fakeCargo{}, root)

			_, err := add(t.Context(), pub, env, func(s string) error {
				if s == step {
					return errors.New("injected failure")
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "injected failure") || strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("err = %v", err)
			}
			// The root Cargo.toml is restored byte for byte, including
			// after the members edit itself.
			assertRestored(t, root, "pub", before)

			if _, err := Add(t.Context(), pub, env); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestAddRealGitRustMetadataGuard(t *testing.T) {
	hermetic(t)
	t.Run("metadata failure rolls the members edit back", func(t *testing.T) {
		pub := publicRepo(t, "pub", rustRepo)
		root := rustWorkspace(t, true)
		before := capture(t, root)

		_, err := Add(t.Context(), pub, realEnv(&fakeCargo{failMeta: true}, root))
		if err == nil || !strings.Contains(err.Error(), "simulated cargo failure") || strings.Contains(err.Error(), "rollback incomplete") {
			t.Fatalf("err = %v", err)
		}
		assertRestored(t, root, "pub", before)
	})
}

// TestAddRealCargoRust exercises the full Rust path with real cargo and a
// workspace produced by the real za init. Opt-in like the other real-tool
// integration tests: it needs git, go, cargo and rustc on PATH.
func TestAddRealCargoRust(t *testing.T) {
	hermetic(t)
	if os.Getenv("ZA_INTEGRATION") == "" {
		t.Skip("opt-in test: set ZA_INTEGRATION=1 (requires cargo and rustc >= 1.85.0)")
	}
	for _, name := range []string{"cargo", "rustc"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not available", name)
		}
	}

	root := filepath.Join(t.TempDir(), "ws")
	if _, err := initcmd.Run(t.Context(), initcmd.Options{Rust: true, Path: root}, initcmd.Env{Runner: tool.Exec{}}); err != nil {
		t.Fatalf("za init --rust: %v", err)
	}
	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "-m", "bootstrap")

	t.Run("single root crate", func(t *testing.T) {
		pub := publicRepo(t, "pub", rustRepo)
		res, err := Add(t.Context(), pub, realEnv(&recorder{}, root))
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if res.Language != "rust" || res.Crate != "pub" {
			t.Errorf("unexpected result %+v", res)
		}
		if manifest := readFile(t, filepath.Join(root, "Cargo.toml")); !strings.Contains(manifest, "members = [\"pub\"]") {
			t.Errorf("Cargo.toml members not updated:\n%s", manifest)
		}
		if present(t, filepath.Join(root, "Cargo.lock")) {
			t.Error("a root Cargo.lock was created; za never writes lockfiles")
		}
		// Commit so the next subtest starts from a clean workspace.
		git(t, root, "add", "--all")
		git(t, root, "commit", "--quiet", "-m", "add pub")
	})

	t.Run("nested virtual workspace rejected", func(t *testing.T) {
		xesubst := publicRepo(t, "xesubst", xesubstRepo)
		before := capture(t, root)
		_, err := Add(t.Context(), xesubst, realEnv(&recorder{}, root))
		if err == nil || !strings.Contains(err.Error(), "declares a [workspace] table") || strings.Contains(err.Error(), "rollback incomplete") {
			t.Fatalf("err = %v", err)
		}
		assertRestored(t, root, "xesubst", before)
	})
}
