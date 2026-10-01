package initcmd

import (
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
// interpreter, or the network.
func TestRealTools(t *testing.T) {
	if testing.Short() || os.Getenv("ZA_INTEGRATION") != "1" {
		t.Skip("set ZA_INTEGRATION=1 (and omit -short) to run against real git/go/uv")
	}
	t.Setenv("UV_OFFLINE", "1")

	tests := []struct {
		name string
		opts Options
	}{
		{"go", Options{Go: true}},
		{"python", Options{Python: true}},
		{"polyglot", Options{Go: true, Python: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, needed := range map[string]bool{"git": true, "go": tt.opts.Go, "uv": tt.opts.Python} {
				if _, err := exec.LookPath(name); needed && err != nil {
					t.Skipf("%s not available", name)
				}
			}
			root := filepath.Join(t.TempDir(), "ws")
			tt.opts.Path = root
			env := Env{Runner: tool.Exec{}, Getwd: os.Getwd}
			if _, err := Run(t.Context(), tt.opts, env); err != nil {
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
			if want := fmt.Sprintf("schema = 1\n\n[workspace]\ngo = %t\npython = %t\n", tt.opts.Go, tt.opts.Python); err != nil || string(manifest) != want {
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
		})
	}
}
