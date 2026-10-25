package project

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tnotstar/go-za/internal/manifest"
	"github.com/tnotstar/go-za/internal/tool"
)

// rollbackTimeout bounds rollback, which keeps running after cancellation.
const rollbackTimeout = time.Minute

// Env holds the process-level dependencies of Add.
type Env struct {
	Runner tool.Runner
	Getwd  func() (string, error)
	Now    func() time.Time
}

// Result describes a successfully added project.
type Result struct {
	ID       string
	URL      string
	Module   string // module path from go.mod; empty if it could not be read
	OpenSpec bool
	Gitlink  string
}

// Summary renders the success report.
func (r Result) Summary() string {
	openspec := "bootstrap pending"
	if r.OpenSpec {
		openspec = r.ID + "/openspec/"
	}
	module := "added to go.work"
	if r.Module != "" {
		module = r.Module + " (added to go.work)"
	}
	return fmt.Sprintf(`Added project %q
  Repository: %s
  Project:    %s/
  Context:    %s/%s/
  Go module:  %s
  OpenSpec:   %s
  Gitlink:    %s

Changes are staged but not committed.
`, r.ID, r.URL, r.ID, contextDir, r.ID, module, openspec, r.Gitlink)
}

// Add registers repoURL as a top-level Git submodule of the enclosing za
// workspace, adds it to go.work and creates its private context and route.
//
// Everything is validated before the first mutation. If a later step fails
// or ctx is cancelled, Add rolls the workspace back to its clean committed
// state on a best-effort basis.
func Add(ctx context.Context, repoURL string, env Env) (Result, error) {
	return add(ctx, repoURL, env, nil)
}

// add is Add with a test hook that may fail the run after a named step.
func add(ctx context.Context, repoURL string, env Env, inject func(step string) error) (Result, error) {
	a, err := prepare(ctx, repoURL, env)
	if err != nil {
		return Result{}, err
	}
	a.inject = inject

	res, err := a.run()
	if err != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if rerr := a.rollback(rctx); rerr != nil {
			return Result{}, fmt.Errorf("%w\nwarning: rollback incomplete: %v", err, rerr)
		}
		return Result{}, err
	}
	return res, nil
}

// snapshot is a tracked file's content before the run, or nil if absent.
type snapshot struct {
	rel     string
	content []byte
	mode    os.FileMode
}

type adder struct {
	ctx    context.Context
	runner tool.Runner
	now    func() time.Time
	inject func(step string) error

	root, url, id string
	templates     map[string][]byte
	agents        []byte // AGENTS.md with the new route
	saved         []snapshot
	gitDir        string   // absolute common Git directory of the workspace
	moduleDirs    []string // absolute Git directories the submodule may use
}

// prepare runs every check that does not mutate the workspace.
func prepare(ctx context.Context, repoURL string, env Env) (*adder, error) {
	id, err := DeriveID(repoURL)
	if err != nil {
		return nil, err
	}
	wd, err := env.Getwd()
	if err != nil {
		return nil, fmt.Errorf("determine current directory: %w", err)
	}
	root, err := manifest.Find(wd)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(root)
	if err != nil {
		return nil, err
	}
	if !m.Workspace.Go {
		return nil, errors.New("za project add currently requires a Go-enabled workspace (workspace.go = true in za.toml)")
	}
	for _, name := range []string{"git", "go"} {
		if _, err := env.Runner.LookPath(name); err != nil {
			return nil, fmt.Errorf("za project add requires executable %q, which was not found in PATH", name)
		}
	}

	a := &adder{ctx: ctx, runner: env.Runner, now: env.Now, root: root, url: repoURL, id: id}
	if err := a.preflightGit(); err != nil {
		return nil, err
	}
	if err := a.preflightPaths(); err != nil {
		return nil, err
	}
	if err := a.preflightFiles(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *adder) git(args ...string) ([]byte, error) {
	return a.runner.Output(a.ctx, tool.Command{Name: "git", Args: args, Dir: a.root})
}

func (a *adder) preflightGit() error {
	out, err := a.git("rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("workspace %s is not a Git working tree: %w", a.root, err)
	}
	if same, err := sameDir(strings.TrimSpace(string(out)), a.root); err != nil || !same {
		return fmt.Errorf("workspace %s is not the top level of its Git working tree (%s)", a.root, strings.TrimSpace(string(out)))
	}
	if _, err := a.git("rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return errors.New("za project add requires the workspace bootstrap to be committed first")
	}
	if dirty, err := a.status(); err != nil {
		return err
	} else if len(dirty) > 0 {
		return fmt.Errorf("za project add requires a clean workspace; commit or stash these changes first: %s", strings.Join(dirty, ", "))
	}

	out, err = a.git("rev-parse", "--path-format=absolute", "--git-common-dir", "--git-path", "modules/"+a.id)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return fmt.Errorf("unexpected output from git rev-parse: %q", out)
	}
	a.gitDir, a.moduleDirs = filepath.Clean(lines[0]), []string{filepath.Clean(lines[1])}
	if ok, err := exists(a.moduleDirs[0]); err != nil || ok {
		return fmt.Errorf("stale submodule Git directory %s exists; remove it before adding project %q", a.moduleDirs[0], a.id)
	}
	if section, err := a.hasConfigSection(); err != nil {
		return err
	} else if section {
		return fmt.Errorf("stale submodule configuration for %q exists in the workspace Git config; remove it with \"git config --remove-section submodule.%s\"", a.id, a.id)
	}

	if ok, err := exists(filepath.Join(a.root, ".gitmodules")); err != nil {
		return err
	} else if ok {
		out, err := a.git("config", "--file", ".gitmodules", "--null", "--list")
		if err != nil {
			return fmt.Errorf("read .gitmodules: %w", err)
		}
		for entry := range strings.SplitSeq(string(out), "\x00") {
			key, value, _ := strings.Cut(entry, "\n")
			name, ok := strings.CutPrefix(key, "submodule.")
			if !ok {
				continue
			}
			if strings.HasSuffix(key, ".path") && path.Clean(value) == a.id {
				return fmt.Errorf(".gitmodules already registers a submodule at path %q", a.id)
			}
			if i := strings.LastIndex(name, "."); i >= 0 && name[:i] == a.id {
				return fmt.Errorf(".gitmodules already registers a submodule named %q", a.id)
			}
		}
	}
	return nil
}

// status lists every staged, unstaged or untracked path, honoring ignore
// rules but no configuration that hides untracked files or submodule changes.
func (a *adder) status() ([]string, error) {
	return a.statusIn(a.root)
}

func (a *adder) statusIn(dir string) ([]string, error) {
	out, err := a.runner.Output(a.ctx, tool.Command{
		Name: "git",
		Args: []string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none"},
		Dir:  dir,
	})
	if err != nil {
		return nil, fmt.Errorf("inspect Git status: %w", err)
	}
	var entries []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if e == "" {
			continue
		}
		entries = append(entries, e)
		if e[0] == 'R' || e[0] == 'C' {
			i++ // the rename source follows as its own field
		}
	}
	return entries, nil
}

func (a *adder) hasConfigSection() (bool, error) {
	out, err := a.git("config", "--local", "--null", "--list")
	if err != nil {
		return false, fmt.Errorf("read workspace Git config: %w", err)
	}
	prefix := "submodule." + a.id + "."
	for entry := range strings.SplitSeq(string(out), "\x00") {
		if strings.HasPrefix(entry, prefix) {
			if key, _, _ := strings.Cut(entry, "\n"); !strings.Contains(key[len(prefix):], ".") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *adder) preflightPaths() error {
	for _, rel := range []string{a.id, path.Join(contextDir, a.id), path.Join(".worktrees", a.id)} {
		ok, err := exists(filepath.Join(a.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("cannot add project %q: %s already exists", a.id, rel)
		}
	}
	return nil
}

// preflightFiles reads every file the run edits or derives content from,
// and renders all content that does not depend on the cloned repository.
func (a *adder) preflightFiles() error {
	if info, err := os.Lstat(filepath.Join(a.root, "go.work")); err != nil || !info.Mode().IsRegular() {
		return errors.New("the Go-enabled workspace has no go.work at its root; recreate it with \"go work init\"")
	}

	for _, rel := range []string{".gitmodules", "go.work", "go.work.sum", "AGENTS.md"} {
		s := snapshot{rel: rel}
		p := filepath.Join(a.root, rel)
		info, err := os.Lstat(p)
		switch {
		case err == nil && !info.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file", rel)
		case err == nil:
			if s.content, err = os.ReadFile(p); err != nil {
				return err
			}
			s.mode = info.Mode().Perm()
		case !os.IsNotExist(err):
			return err
		}
		a.saved = append(a.saved, s)
	}

	agents := a.original("AGENTS.md")
	if agents == nil {
		return errors.New("workspace has no AGENTS.md")
	}
	var err error
	if a.agents, err = addRoute(agents, a.id); err != nil {
		return err
	}

	if a.templates, err = readTemplates(a.root); err != nil {
		return err
	}
	// Prove the templates accept every fact combination before mutating.
	for _, openspec := range []bool{false, true} {
		for _, goSum := range []bool{false, true} {
			f := facts{ID: a.id, Gitlink: strings.Repeat("0", 40), Date: "2006-01-02", OpenSpec: openspec, GoSum: goSum}
			if _, err := renderContext(a.templates, f); err != nil {
				return err
			}
		}
	}
	return nil
}

// original returns the snapshotted content of rel, or nil if it was absent.
func (a *adder) original(rel string) []byte {
	for _, s := range a.saved {
		if s.rel == rel {
			return s.content
		}
	}
	return nil
}

// step runs fn unless the run was cancelled, then the test hook, if any.
func (a *adder) step(name string, fn func() error) error {
	if err := a.ctx.Err(); err != nil {
		return fmt.Errorf("project add interrupted: %w", err)
	}
	if err := fn(); err != nil {
		return err
	}
	if a.inject != nil {
		return a.inject(name)
	}
	return nil
}

func (a *adder) run() (Result, error) {
	res := Result{ID: a.id, URL: a.url}
	projectDir := filepath.Join(a.root, a.id)
	var ctxFacts facts

	err := a.step("submodule", func() error {
		if err := a.runner.Run(a.ctx, tool.Command{
			Name: "git", Args: []string{"submodule", "add", "--", a.url, a.id}, Dir: a.root,
		}); err != nil {
			return fmt.Errorf("add submodule: %w", err)
		}
		out, err := a.runner.Output(a.ctx, tool.Command{
			Name: "git", Args: []string{"rev-parse", "--absolute-git-dir"}, Dir: projectDir,
		})
		if err != nil {
			return fmt.Errorf("locate submodule Git directory: %w", err)
		}
		if dir := filepath.Clean(strings.TrimSpace(string(out))); !slices.Contains(a.moduleDirs, dir) {
			a.moduleDirs = append(a.moduleDirs, dir)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	err = a.step("validate", func() error {
		info, err := os.Lstat(filepath.Join(projectDir, "go.mod"))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("project %q is not a supported root Go module: %s/go.mod not found (nested modules are not supported yet)", a.id, a.id)
		}
		if res.Gitlink, err = a.gitlink(); err != nil {
			return err
		}
		if info, err := os.Stat(filepath.Join(projectDir, "openspec")); err == nil && info.IsDir() {
			res.OpenSpec = true
		}
		goSum, err := exists(filepath.Join(projectDir, "go.sum"))
		if err != nil {
			return err
		}
		res.Module = modulePath(filepath.Join(projectDir, "go.mod"))
		ctxFacts = facts{ID: a.id, Gitlink: res.Gitlink, Date: a.now().Format(time.DateOnly), OpenSpec: res.OpenSpec, GoSum: goSum}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	err = a.step("gowork", func() error {
		// An explicit GOWORK pins the workspace file, so no other go.work
		// and no inherited GOWORK value can be selected instead.
		if err := a.runner.Run(a.ctx, tool.Command{
			Name: "go", Args: []string{"work", "use", "./" + a.id}, Dir: a.root,
			Env: []string{"GOWORK=" + filepath.Join(a.root, "go.work")},
		}); err != nil {
			return fmt.Errorf("add project to go.work: %w", err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	err = a.step("context", func() error {
		files, err := renderContext(a.templates, ctxFacts)
		if err != nil {
			return err
		}
		return writeContext(a.root, a.id, files)
	})
	if err != nil {
		return Result{}, err
	}

	err = a.step("routing", func() error {
		return os.WriteFile(filepath.Join(a.root, "AGENTS.md"), a.agents, 0o644)
	})
	if err != nil {
		return Result{}, err
	}

	if err := a.step("stage", a.stage); err != nil {
		return Result{}, err
	}
	return res, nil
}

// gitlink returns the full commit ID staged for the submodule.
func (a *adder) gitlink() (string, error) {
	out, err := a.git("ls-files", "--stage", "-z", "--", a.id)
	if err != nil {
		return "", fmt.Errorf("read submodule gitlink: %w", err)
	}
	entry, _, _ := strings.Cut(string(out), "\x00")
	meta, p, _ := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	if p != a.id || len(fields) != 3 || fields[0] != "160000" || !isHexSHA(fields[1]) {
		return "", fmt.Errorf("%s is not staged as a submodule gitlink (git ls-files: %q)", a.id, entry)
	}
	return fields[1], nil
}

func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// stage stages exactly the project-add change and verifies that nothing
// else changed, including inside the public submodule.
func (a *adder) stage() error {
	ctxRel := path.Join(contextDir, a.id)
	paths := []string{".gitmodules", a.id, "go.work", "AGENTS.md", ctxRel}
	if ok, err := exists(filepath.Join(a.root, "go.work.sum")); err != nil {
		return err
	} else if ok {
		paths = append(paths, "go.work.sum")
	}
	if _, err := a.git(append([]string{"add", "--"}, paths...)...); err != nil {
		return fmt.Errorf("stage project addition: %w", err)
	}

	if dirty, err := a.statusIn(filepath.Join(a.root, a.id)); err != nil {
		return err
	} else if len(dirty) > 0 {
		return fmt.Errorf("the public project %s was modified during the addition: %s", a.id, strings.Join(dirty, ", "))
	}
	entries, err := a.status()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if len(e) < 4 {
			return fmt.Errorf("unexpected Git status entry %q", e)
		}
		xy, p := e[:2], e[3:]
		expected := slices.Contains(paths, p) || strings.HasPrefix(p, ctxRel+"/")
		if !expected || xy[1] != ' ' || (xy[0] != 'A' && xy[0] != 'M') {
			return fmt.Errorf("unexpected change after staging: %q", e)
		}
	}
	return nil
}

// rollback restores the clean committed state the run started from. It
// removes only paths that preflight proved absent and restores only files
// it snapshotted.
func (a *adder) rollback(ctx context.Context) error {
	a.ctx = ctx
	var errs []error

	// The index matched HEAD before the run, so resetting it is exact.
	if _, err := a.git("reset", "--quiet", "--no-recurse-submodules"); err != nil {
		errs = append(errs, fmt.Errorf("reset index: %w", err))
	}
	if section, err := a.hasConfigSection(); err != nil {
		errs = append(errs, err)
	} else if section {
		if _, err := a.git("config", "--local", "--remove-section", "submodule."+a.id); err != nil {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(a.root, a.id)); err != nil {
		errs = append(errs, err)
	}
	for _, dir := range a.moduleDirs {
		rel, err := filepath.Rel(a.gitDir, dir)
		if err != nil || rel == "." || !strings.HasPrefix(rel, "modules"+string(filepath.Separator)) {
			errs = append(errs, fmt.Errorf("left submodule Git directory %s in place: it is outside %s", dir, filepath.Join(a.gitDir, "modules")))
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(a.root, filepath.FromSlash(contextDir), a.id)); err != nil {
		errs = append(errs, err)
	}
	for _, s := range a.saved {
		if err := restore(a.root, s); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		if dirty, err := a.status(); err != nil {
			errs = append(errs, err)
		} else if len(dirty) > 0 {
			errs = append(errs, fmt.Errorf("workspace is not clean: %s", strings.Join(dirty, ", ")))
		}
	}
	return errors.Join(errs...)
}

func restore(root string, s snapshot) error {
	p := filepath.Join(root, s.rel)
	if s.content == nil {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, s.content) {
		return nil
	}
	if err := os.WriteFile(p, s.content, s.mode); err != nil {
		return fmt.Errorf("restore %s: %w", s.rel, err)
	}
	return nil
}

// modulePath returns the module path declared in a go.mod file, or "" if it
// cannot be determined by a simple scan. It is informational only.
func modulePath(gomod string) string {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "//")
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			return strings.Trim(strings.TrimSpace(rest), `"`+"`")
		}
	}
	return ""
}

// sameDir reports whether a and b name the same existing directory.
func sameDir(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}
