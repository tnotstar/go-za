package project

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tnotstar/go-za/internal/initcmd"
	"github.com/tnotstar/go-za/internal/manifest"
	"github.com/tnotstar/go-za/internal/tool"
	"github.com/tnotstar/go-za/internal/tool/tooltest"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/golden")

var fixedNow = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

// recorder runs real commands, records them and may fail selected ones.
type recorder struct {
	tool.Exec
	calls []tool.Command
	fail  func(tool.Command) error
}

func (r *recorder) Run(ctx context.Context, c tool.Command) error {
	_, err := r.Output(ctx, c)
	return err
}

func (r *recorder) Output(ctx context.Context, c tool.Command) ([]byte, error) {
	r.calls = append(r.calls, c)
	if r.fail != nil {
		if err := r.fail(c); err != nil {
			return nil, err
		}
	}
	return r.Exec.Output(ctx, c)
}

// mutating reports the recorded commands that may change the workspace.
func mutating(calls []tool.Command) []tool.Command {
	var out []tool.Command
	for _, c := range calls {
		if c.Name != "git" || len(c.Args) == 0 {
			out = append(out, c)
			continue
		}
		switch c.Args[0] {
		case "rev-parse", "status", "ls-files":
			continue
		case "config":
			if !slices.Contains(c.Args, "--remove-section") {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// hermetic isolates Git and Go from user and system configuration. Local
// file transport for submodules is allowed only in this test configuration.
func hermetic(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real Git test skipped in -short mode")
	}
	for _, name := range []string{"git", "go"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not available", name)
		}
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitconfig")
	content := "[user]\n\tname = za test\n\temail = za@example.invalid\n[init]\n\tdefaultBranch = main\n[protocol \"file\"]\n\tallow = always\n[commit]\n\tgpgsign = false\n"
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func present(t *testing.T, path string) bool {
	t.Helper()
	ok, err := exists(path)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// publicRepo creates a committed repository named name with files.
func publicRepo(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for rel, content := range files {
		writeFile(t, filepath.Join(dir, rel), content)
	}
	git(t, dir, "init", "--quiet")
	git(t, dir, "add", "--all")
	git(t, dir, "commit", "--quiet", "-m", "initial")
	return dir
}

var goRepo = map[string]string{
	"go.mod": "module example.com/pub\n\ngo 1.24\n",
	"pub.go": "package pub\n",
}

// withOpenSpec adds an OpenSpec layout and a go.sum to files.
func withOpenSpec(files map[string]string) map[string]string {
	out := map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/specs/.gitkeep": "", "go.sum": ""}
	maps.Copy(out, files)
	return out
}

// workspace runs the real za init and commits the bootstrap.
func workspace(t *testing.T, commit bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	if _, err := initcmd.Run(t.Context(), initcmd.Options{Go: true, Path: root}, initcmd.Env{Runner: tool.Exec{}}); err != nil {
		t.Fatalf("za init: %v", err)
	}
	if commit {
		git(t, root, "add", "--all")
		git(t, root, "commit", "--quiet", "-m", "bootstrap")
	}
	return root
}

func realEnv(r tool.Runner, wd string) Env {
	return Env{Runner: r, Getwd: func() (string, error) { return wd, nil }, Now: fixedNow}
}

// state captures everything a failed add must leave unchanged.
type state struct {
	head, status, gitConfig string
	files                   map[string]string
}

var trackedFiles = []string{"AGENTS.md", "go.work", "za.toml"}

func capture(t *testing.T, root string) state {
	t.Helper()
	s := state{
		head:      git(t, root, "rev-parse", "HEAD"),
		status:    git(t, root, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none"),
		gitConfig: readFile(t, filepath.Join(root, ".git", "config")),
		files:     map[string]string{},
	}
	for _, rel := range append(trackedFiles, ".gitmodules", "go.work.sum") {
		if data, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
			s.files[rel] = string(data)
		}
	}
	return s
}

func assertRestored(t *testing.T, root, id string, before state) {
	t.Helper()
	after := capture(t, root)
	if after.head != before.head {
		t.Errorf("HEAD changed: %s -> %s", before.head, after.head)
	}
	if after.status != "" {
		t.Errorf("workspace not clean after rollback:\n%s", after.status)
	}
	if after.gitConfig != before.gitConfig {
		t.Errorf(".git/config not restored:\nbefore:\n%s\nafter:\n%s", before.gitConfig, after.gitConfig)
	}
	for rel, want := range before.files {
		if after.files[rel] != want {
			t.Errorf("%s not restored byte for byte", rel)
		}
	}
	for rel := range after.files {
		if _, ok := before.files[rel]; !ok {
			t.Errorf("%s left behind", rel)
		}
	}
	for _, rel := range []string{id, ".agents/context/projects/" + id, ".git/modules/" + id} {
		if present(t, filepath.Join(root, rel)) {
			t.Errorf("%s left behind", rel)
		}
	}
}

func TestAddRealGitSuccess(t *testing.T) {
	hermetic(t)
	for _, variant := range []string{"openspec-pending", "openspec-present"} {
		t.Run(variant, func(t *testing.T) {
			files := goRepo
			if variant == "openspec-present" {
				files = withOpenSpec(goRepo)
			}
			pub := publicRepo(t, "pub", files)
			pubHead := strings.TrimSpace(git(t, pub, "rev-parse", "HEAD"))
			root := workspace(t, true)
			before := capture(t, root)
			rec := &recorder{}

			// Run from a descendant directory of the workspace.
			res, err := Add(t.Context(), pub, realEnv(rec, filepath.Join(root, ".agents", "context")))
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			if res.ID != "pub" || res.Gitlink != pubHead || res.Module != "example.com/pub" || res.OpenSpec != (variant == "openspec-present") {
				t.Errorf("unexpected result %+v (public HEAD %s)", res, pubHead)
			}
			summary := res.Summary()
			for _, want := range []string{`Added project "pub"`, "Project:    pub/", "Gitlink:    " + pubHead, "staged but not committed"} {
				if !strings.Contains(summary, want) {
					t.Errorf("summary lacks %q:\n%s", want, summary)
				}
			}

			// Native commands, run directly from the workspace root.
			wantCalls := []tool.Command{
				{Name: "git", Args: []string{"submodule", "add", "--", pub, "pub"}, Dir: root},
				{Name: "go", Args: []string{"work", "use", "./pub"}, Dir: root, Env: []string{"GOWORK=" + filepath.Join(root, "go.work")}},
			}
			for _, want := range wantCalls {
				if !slices.ContainsFunc(rec.calls, func(c tool.Command) bool {
					return c.Name == want.Name && slices.Equal(c.Args, want.Args) && c.Dir == want.Dir && slices.Equal(c.Env, want.Env)
				}) {
					t.Errorf("missing native call %+v", want)
				}
			}
			for _, c := range rec.calls {
				if c.Name != "git" && c.Name != "go" {
					t.Errorf("unexpected executable %q", c.Name)
				}
				if slices.Contains(c.Args, "--force") || slices.Contains(c.Args, "-b") || slices.Contains(c.Args, "--branch") {
					t.Errorf("forbidden argument in %s", c)
				}
			}

			// Git state: one staged change, no commit, clean public checkout.
			if head := git(t, root, "rev-parse", "HEAD"); head != before.head {
				t.Error("a commit was created")
			}
			staged := strings.Fields(git(t, root, "diff", "--cached", "--name-only"))
			wantStaged := []string{
				".agents/context/projects/pub/INDEX.md",
				".agents/context/projects/pub/constraints.md",
				".agents/context/projects/pub/decisions/.gitkeep",
				".agents/context/projects/pub/source-of-truth.md",
				".agents/context/projects/pub/status.md",
				".agents/context/projects/pub/tools.md",
				".gitmodules", "AGENTS.md", "go.work", "pub",
			}
			if !slices.Equal(staged, wantStaged) {
				t.Errorf("staged = %v, want %v", staged, wantStaged)
			}
			if out := git(t, root, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none"); strings.ContainsAny(strings.Join(lineYs(out), ""), "MD?") {
				t.Errorf("unstaged or untracked changes remain:\n%s", out)
			}
			if out := git(t, filepath.Join(root, "pub"), "status", "--porcelain", "--untracked-files=all"); out != "" {
				t.Errorf("public project modified:\n%s", out)
			}
			if ls := git(t, root, "ls-files", "--stage", "pub"); !strings.HasPrefix(ls, "160000 "+pubHead+" ") {
				t.Errorf("gitlink = %q, want %s", ls, pubHead)
			}
			if got := git(t, root, "config", "--file", ".gitmodules", "--get-regexp", "^submodule\\."); got != "submodule.pub.path pub\nsubmodule.pub.url "+pub+"\n" {
				t.Errorf(".gitmodules = %q", got)
			}
			if !strings.Contains(readFile(t, filepath.Join(root, "go.work")), "./pub") {
				t.Errorf("go.work lacks ./pub:\n%s", readFile(t, filepath.Join(root, "go.work")))
			}
			if got := readFile(t, filepath.Join(root, "za.toml")); got != before.files["za.toml"] {
				t.Errorf("za.toml modified:\n%s", got)
			}
			wantAgents, err := addRoute([]byte(before.files["AGENTS.md"]), "pub")
			if err != nil || readFile(t, filepath.Join(root, "AGENTS.md")) != string(wantAgents) {
				t.Errorf("AGENTS.md is not the original with only the route added (%v)", err)
			}
			if variant == "openspec-pending" && present(t, filepath.Join(root, "pub", "openspec")) {
				t.Error("openspec/ was created in the public project")
			}

			// Private context, compared against golden files with the
			// variable gitlink masked.
			ctxDir := filepath.Join(root, ".agents", "context", "projects", "pub")
			if des, err := os.ReadDir(filepath.Join(ctxDir, "decisions")); err != nil || len(des) != 1 || des[0].Name() != ".gitkeep" {
				t.Errorf("decisions/ must hold only .gitkeep: %v %v", des, err)
			}
			for _, name := range contextFiles {
				got := strings.ReplaceAll(readFile(t, filepath.Join(ctxDir, name)), pubHead, "<GITLINK>")
				golden := filepath.Join("testdata", "golden", variant, name+".golden")
				if *update {
					writeFile(t, golden, got)
					continue
				}
				if want := readFile(t, golden); got != want {
					t.Errorf("%s differs from %s (run with -update):\n%s", name, golden, got)
				}
			}
			status := readFile(t, filepath.Join(ctxDir, "status.md"))
			if !strings.Contains(status, "- Verified against gitlink: `"+pubHead+"`") {
				t.Errorf("status.md lacks the gitlink:\n%s", status)
			}
			if pending := strings.Contains(status, "OpenSpec bootstrap pending"); pending != (variant == "openspec-pending") {
				t.Errorf("status.md bootstrap-pending marker = %t:\n%s", pending, status)
			}
		})
	}
}

// lineYs returns the worktree status column of each porcelain line.
func lineYs(porcelain string) []string {
	var ys []string
	for line := range strings.SplitSeq(strings.TrimSuffix(porcelain, "\n"), "\n") {
		if len(line) >= 2 {
			ys = append(ys, string(line[1]))
		}
	}
	return ys
}

func TestAddRealGitRollback(t *testing.T) {
	hermetic(t)
	steps := []string{"submodule", "validate", "gowork", "context", "routing", "stage"}
	for _, step := range steps {
		t.Run("after "+step, func(t *testing.T) {
			pub := publicRepo(t, "pub", goRepo)
			root := workspace(t, true)
			before := capture(t, root)
			env := realEnv(&recorder{}, root)

			_, err := add(t.Context(), pub, env, func(s string) error {
				if s == step {
					return errors.New("injected failure")
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "injected failure") || strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("err = %v", err)
			}
			assertRestored(t, root, "pub", before)

			// A retry is not blocked by state left by the failed attempt.
			if _, err := Add(t.Context(), pub, env); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestAddRealGitFailures(t *testing.T) {
	hermetic(t)
	tests := []struct {
		name  string
		files map[string]string
		url   func(pub string) string
		fail  func(tool.Command) error
		err   string
	}{
		{
			name:  "no go.mod",
			files: map[string]string{"README.md": "x\n"},
			err:   `project "pub" is not a supported root Go module: pub/go.mod not found`,
		},
		{
			name:  "nested module only",
			files: map[string]string{"services/api/go.mod": "module example.com/api\n\ngo 1.24\n"},
			err:   "pub/go.mod not found",
		},
		{
			name:  "unreachable repository",
			files: goRepo,
			url:   func(pub string) string { return filepath.Join(filepath.Dir(pub), "missing", "pub.git") },
			err:   "add submodule:",
		},
		{
			name:  "go work use fails",
			files: goRepo,
			fail: func(c tool.Command) error {
				if c.Name == "go" {
					return errors.New("simulated go failure")
				}
				return nil
			},
			err: "simulated go failure",
		},
		{
			name:  "git add fails",
			files: goRepo,
			fail: func(c tool.Command) error {
				if c.Name == "git" && c.Args[0] == "add" {
					return errors.New("simulated staging failure")
				}
				return nil
			},
			err: "simulated staging failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := publicRepo(t, "pub", tt.files)
			url := pub
			if tt.url != nil {
				url = tt.url(pub)
			}
			root := workspace(t, true)
			before := capture(t, root)

			_, err := Add(t.Context(), url, realEnv(&recorder{fail: tt.fail}, root))
			if err == nil || !strings.Contains(err.Error(), tt.err) || strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("err = %v, want containing %q", err, tt.err)
			}
			assertRestored(t, root, "pub", before)
			if tt.name == "no go.mod" && present(t, filepath.Join(pub, "go.mod")) {
				t.Error("go.mod was created")
			}
		})
	}
}

func TestAddRealGitCancellation(t *testing.T) {
	hermetic(t)
	pub := publicRepo(t, "pub", goRepo)
	root := workspace(t, true)
	before := capture(t, root)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := add(ctx, pub, realEnv(&recorder{}, root), func(step string) error {
		if step == "submodule" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("err = %v, want cancellation with complete rollback", err)
	}
	assertRestored(t, root, "pub", before)
}

func TestAddRealGitPreflight(t *testing.T) {
	hermetic(t)
	tests := []struct {
		name    string
		commit  bool
		setup   func(t *testing.T, root string)
		err     string
		noCheck bool // the workspace is not a valid Git baseline to compare
	}{
		{name: "no commit", err: "requires the workspace bootstrap to be committed first", noCheck: true},
		{name: "untracked file", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "notes.txt"), "x\n")
		}, err: "requires a clean workspace"},
		{name: "staged change", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "notes.txt"), "x\n")
			git(t, root, "add", "notes.txt")
		}, err: "requires a clean workspace"},
		{name: "modified file", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "AGENTS.md"), "changed\n")
		}, err: "requires a clean workspace"},
		{name: "project directory exists", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "pub", "README.md"), "x\n")
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "pub dir")
		}, err: "pub already exists"},
		{name: "private context exists", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".agents", "context", "projects", "pub", "INDEX.md"), "x\n")
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "ctx")
		}, err: ".agents/context/projects/pub already exists"},
		{name: "worktrees exist", commit: true, setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, ".worktrees", "pub", "task"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, err: ".worktrees/pub already exists"},
		{name: "gitmodules name collision", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"pub\"]\n\tpath = elsewhere\n\turl = x\n")
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "gitmodules")
		}, err: `already registers a submodule named "pub"`},
		{name: "gitmodules path collision", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"other\"]\n\tpath = pub\n\turl = x\n")
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "gitmodules")
		}, err: `already registers a submodule at path "pub"`},
		{name: "stale module directory", commit: true, setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, ".git", "modules", "pub"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, err: "stale submodule Git directory"},
		{name: "stale submodule config", commit: true, setup: func(t *testing.T, root string) {
			git(t, root, "config", "submodule.pub.url", "x")
		}, err: "stale submodule configuration"},
		{name: "routing markers missing", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "AGENTS.md"), "# custom router\n")
			git(t, root, "commit", "--quiet", "-am", "custom")
		}, err: "missing project routing marker"},
		{name: "customized template", commit: true, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".agents", "context", "_global", "templates", "project", "status.md"), "# Status\n")
			git(t, root, "commit", "--quiet", "-am", "custom")
		}, err: "does not match the layout generated by za init"},
		{name: "go.work missing", commit: true, setup: func(t *testing.T, root string) {
			git(t, root, "rm", "--quiet", "go.work")
			git(t, root, "commit", "--quiet", "-m", "no go.work")
		}, err: "has no go.work"},
		{name: "workspace not git top level", setup: func(t *testing.T, root string) {
			// Move the workspace content into a subdirectory of the repository.
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "bootstrap")
			for _, f := range []string{"za.toml", "AGENTS.md", "go.work"} {
				writeFile(t, filepath.Join(root, "inner", f), readFile(t, filepath.Join(root, f)))
			}
			git(t, root, "add", "--all")
			git(t, root, "commit", "--quiet", "-m", "inner")
		}, err: "is not the top level of its Git working tree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := publicRepo(t, "pub", goRepo)
			root := workspace(t, tt.commit)
			if tt.setup != nil {
				tt.setup(t, root)
			}
			wd := root
			if tt.name == "workspace not git top level" {
				wd = filepath.Join(root, "inner")
			}
			var before string
			if !tt.noCheck {
				before = git(t, root, "status", "--porcelain", "--untracked-files=all", "--ignored")
			}
			rec := &recorder{}

			_, err := Add(t.Context(), pub, realEnv(rec, wd))
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("err = %v, want containing %q", err, tt.err)
			}
			if m := mutating(rec.calls); len(m) > 0 {
				t.Errorf("mutating commands ran during preflight: %v", m)
			}
			if !tt.noCheck {
				if after := git(t, root, "status", "--porcelain", "--untracked-files=all", "--ignored"); after != before {
					t.Errorf("workspace changed:\nbefore:\n%s\nafter:\n%s", before, after)
				}
			}
		})
	}
}

func TestAddNotAGitRepository(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "za.toml"), "schema = 1\n\n[workspace]\ngo = true\npython = false\n")
	// Keep git from discovering an enclosing repository of the temp dir.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	rec := &recorder{}
	_, err := Add(t.Context(), "https://example.invalid/org/pub.git", realEnv(rec, root))
	if err == nil || !strings.Contains(err.Error(), "is not a Git working tree") {
		t.Fatalf("err = %v", err)
	}
	if m := mutating(rec.calls); len(m) > 0 {
		t.Errorf("mutating commands ran: %v", m)
	}
}

// Fake-runner tests: failures detected before any native command runs.
func TestAddPreflightWithoutCommands(t *testing.T) {
	goManifest := "schema = 1\n\n[workspace]\ngo = true\npython = false\n"
	tests := []struct {
		name     string
		manifest string // empty: no za.toml at all
		url      string
		missing  string
		err      string
	}{
		{name: "not inside workspace", url: "https://h/o/pub.git", err: "not inside a za workspace"},
		{name: "malformed manifest", manifest: "schema = 1\n[workspace\n", url: "https://h/o/pub.git", err: "invalid workspace manifest"},
		{name: "unsupported schema", manifest: "schema = 2\n", url: "https://h/o/pub.git", err: "unsupported za workspace schema: 2"},
		{name: "missing workspace table", manifest: "schema = 1\n", url: "https://h/o/pub.git", err: "missing [workspace] table"},
		{name: "go disabled", manifest: "schema = 1\n[workspace]\ngo = false\npython = true\n", url: "https://h/o/pub.git", err: "requires a Go-enabled workspace"},
		{name: "missing git", manifest: goManifest, url: "https://h/o/pub.git", missing: "git", err: `requires executable "git"`},
		{name: "missing go", manifest: goManifest, url: "https://h/o/pub.git", missing: "go", err: `requires executable "go"`},
		{name: "invalid project id", manifest: goManifest, url: "https://h/o/..", err: "cannot derive a project ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			// go.work alone must never identify a workspace.
			writeFile(t, filepath.Join(root, "go.work"), "go 1.24\n")
			if tt.manifest != "" {
				writeFile(t, filepath.Join(root, "za.toml"), tt.manifest)
			}
			child := filepath.Join(root, "child")
			if err := os.Mkdir(child, 0o755); err != nil {
				t.Fatal(err)
			}
			f := &tooltest.Fake{Missing: map[string]bool{tt.missing: true}}
			if _, err := manifest.Find(filepath.Dir(root)); tt.manifest == "" && err == nil {
				t.Skip("a za.toml encloses the temporary directory")
			}

			_, err := Add(t.Context(), tt.url, realEnv(f, child))
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("err = %v, want containing %q", err, tt.err)
			}
			if len(f.Calls) > 0 {
				t.Errorf("native commands ran: %v", f.Calls)
			}
		})
	}
}

func TestAddPreflightGitFailureRunsNoMutation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "za.toml"), "schema = 1\n\n[workspace]\ngo = true\npython = false\n")
	f := &tooltest.Fake{Respond: func(c tool.Command) ([]byte, error) {
		return nil, fmt.Errorf("%s: fatal: not a git repository", c)
	}}
	_, err := Add(t.Context(), "https://h/o/pub.git", realEnv(f, root))
	if err == nil || !strings.Contains(err.Error(), "is not a Git working tree") {
		t.Fatalf("err = %v", err)
	}
	if len(f.Calls) != 1 || f.Calls[0].Name != "git" || f.Calls[0].Args[0] != "rev-parse" || f.Calls[0].Dir != root {
		t.Errorf("calls = %v", f.Calls)
	}
}

func TestRenderContextRejectsChangedTemplates(t *testing.T) {
	templates := map[string][]byte{}
	for _, name := range contextFiles {
		templates[name] = []byte("# " + name + ": <project-id>\n")
	}
	_, err := renderContext(templates, facts{ID: "x", Gitlink: strings.Repeat("a", 40), Date: "2026-10-01"})
	if err == nil || !strings.Contains(err.Error(), "does not match the layout generated by za init") {
		t.Fatalf("err = %v", err)
	}
}
