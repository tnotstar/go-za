package scaffold

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/golden")

var modes = []struct {
	name string
	opts Options
}{
	{"go", Options{Go: true}},
	{"python", Options{Python: true}},
	{"rust", Options{Rust: true}},
	{"polyglot", Options{Go: true, Python: true}},
	{"all", Options{Go: true, Python: true, Rust: true}},
}

// TestGolden compares every rendered file, per workspace mode, with
// testdata/golden/<mode>/<path>.golden. Regenerate with:
//
//	go test ./internal/scaffold -update
func TestGolden(t *testing.T) {
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			s, err := Render(m.opts)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			dir := filepath.Join("testdata", "golden", m.name)
			if *update {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
				for _, f := range s.Files {
					p := goldenPath(dir, f.Path)
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, f.Content, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}

			var rendered []string
			for _, f := range s.Files {
				rendered = append(rendered, f.Path)
				want, err := os.ReadFile(goldenPath(dir, f.Path))
				if err != nil {
					t.Errorf("%s: %v (run with -update)", f.Path, err)
					continue
				}
				if !bytes.Equal(f.Content, want) {
					t.Errorf("%s differs from golden file:\n--- got ---\n%s\n--- want ---\n%s", f.Path, f.Content, want)
				}
			}

			var golden []string
			err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, err := filepath.Rel(dir, p)
				golden = append(golden, strings.TrimSuffix(filepath.ToSlash(rel), ".golden"))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(rendered)
			slices.Sort(golden)
			if !slices.Equal(rendered, golden) {
				t.Errorf("rendered files %v\ndo not match golden files %v", rendered, golden)
			}
		})
	}
}

func goldenPath(dir, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel)+".golden")
}

func TestRenderIsDeterministic(t *testing.T) {
	for _, m := range modes {
		a, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Files) != len(b.Files) {
			t.Fatalf("%s: file count differs", m.name)
		}
		for i := range a.Files {
			if a.Files[i].Path != b.Files[i].Path || !bytes.Equal(a.Files[i].Content, b.Files[i].Content) {
				t.Errorf("%s: %s rendered differently", m.name, a.Files[i].Path)
			}
		}
	}
}

// Hard size ceilings from the architecture's context budget: the root guide
// 4 KiB, the common guide 3 KiB, the project guide template 3 KiB.
var ceilings = map[string]int{
	"AGENTS.md":                                     4 << 10,
	".agents/context/AGENTS.md":                     3 << 10,
	".agents/context/templates/issue.md":            12 << 10,
	".agents/context/templates/project/AGENTS.md":   3 << 10,
	".agents/context/templates/project/tools.md":    6 << 10,
	".agents/context/templates/project/sot.md":      6 << 10,
	".agents/context/templates/project/status.md":   2 << 10,
	".agents/context/templates/project/adr.md":      3 << 10,
	".agents/context/templates/project/decision.md": 6 << 10,
}

func TestContextSizeBudgets(t *testing.T) {
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range s.Files {
			limit, ok := ceilings[f.Path]
			if !ok && strings.HasPrefix(f.Path, ".agents/context/") && !strings.Contains(f.Path, "/templates/") {
				limit, ok = 8<<10, true
			}
			if ok && len(f.Content) > limit {
				t.Errorf("%s (%s) is %d bytes, exceeds ceiling of %d", m.name, f.Path, len(f.Content), limit)
			}
		}
	}
}

func TestRenderedContentInvariants(t *testing.T) {
	obsolete := []string{".agents/context/_global/", ".agents/context/projects/"}
	// Schema-3 files removed by schema 4: rendering them, or linking to
	// them, would leave stale manuals and stale links in new workspaces.
	stale := []string{
		"INDEX.md", "principles.md", "roles.md", "glossary.md",
		"go-standards.md", "python-standards.md", "rust-standards.md",
		"constraints.md", "source-of-truth.md",
	}
	// Retired instruction wording: rule strength belongs to COMMON, and the
	// pre-correction templates carried both the wrong template suffix and a
	// blanket verification gate.
	banned := []string{"global MUST", "global DEFAULT", "AGENTS.md.temp", "Context verified:"}
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range s.Files {
			if bytes.Contains(f.Content, []byte("\r")) {
				t.Errorf("%s/%s contains CR characters", m.name, f.Path)
			}
			if len(f.Content) > 0 && !bytes.HasSuffix(f.Content, []byte("\n")) {
				t.Errorf("%s/%s lacks a final newline", m.name, f.Path)
			}
			if strings.HasPrefix(f.Path, ".agents/bin") || strings.HasSuffix(f.Path, ".sh") {
				t.Errorf("%s/%s: za must not generate management scripts", m.name, f.Path)
			}
			if strings.Contains(f.Path, "openspec") {
				t.Errorf("%s/%s: za must not generate OpenSpec content", m.name, f.Path)
			}
			for _, o := range obsolete {
				if strings.Contains(f.Path, o) || bytes.Contains(f.Content, []byte(o)) {
					t.Errorf("%s/%s: references obsolete pre-v5 context path %s", m.name, f.Path, o)
				}
			}
			for _, name := range stale {
				if bytes.Contains(f.Content, []byte(name)) {
					t.Errorf("%s/%s: references the removed schema-3 file %s", m.name, f.Path, name)
				}
			}
			for _, phrase := range banned {
				if bytes.Contains(f.Content, []byte(phrase)) {
					t.Errorf("%s/%s: contains retired wording %q", m.name, f.Path, phrase)
				}
			}
		}
	}
}

// TestScaffoldInstructionContracts pins short semantic anchors of the
// corrected instruction set: the scoped freshness gate, the scoped
// verification model, the COMMON/GLOBAL boundary, explicit multi-project
// access, path semantics and the unified decision lifecycle. Anchors are
// deliberately short phrases, not paragraphs.
func TestScaffoldInstructionContracts(t *testing.T) {
	s, err := Render(Options{Go: true, Python: true, Rust: true})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range s.Files {
		byPath[f.Path] = string(f.Content)
	}
	anchors := map[string][]string{
		".agents/context/policy.md": {
			// Scoped freshness gate: no blanket write prohibition.
			"does not independently block",
			"Pause only dependent implementation",
			"never synchronize a pin, reset a checkout or update a gitlink",
			// Worktree traversal and write locations.
			"Recursive search is allowed inside the selected task worktree",
			"never traverse the `.worktrees/` collection recursively",
			"Project-source changes happen in a task worktree",
			"belong in the meta-repository",
			// COMMON/GLOBAL boundary.
			"Private context owned by this workspace",
			"neither requires, provisions, modifies nor recursively inspects",
			"empty personal profile",
			"surfaced for resolution",
			// Rule-strength vocabulary.
			"COMMON MUST > accepted project decision > COMMON DEFAULT",
		},
		".agents/context/AGENTS.md": {
			// Explicit multi-project access with implicit access prohibited.
			"every project the task names",
			"never read the context of a project the task does not name",
			"empty personal profile",
			"supported loading mechanisms",
		},
		".agents/context/templates/project/AGENTS.md": {
			// Path semantics: workspace-relative vs document-relative.
			"workspace-relative",
			"against this guide's own directory",
			// Scoped gate and pause conditions in the project guide.
			"neither stops the task by itself",
			"a verified scope supports work while unrelated scopes stay pending",
			"Pause only dependent implementation when the task base is ambiguous",
			// Constraint routing before design or source changes.
			"architectural constraints identified in",
		},
		".agents/context/templates/project/status.md": {
			// Scoped verification model.
			"derived from these rows and never overrides them",
			"Registration records the observed pin and leaves every scope pending",
			"A verified scope supports work on that scope while unrelated scopes stay pending",
			"Matching SHAs alone are not evidence",
			"triggers reassessment of the affected scope only",
			"does not invalidate technical verification",
			"keep the previous row as prior evidence",
			// When to update.
			"Leave it unchanged otherwise",
		},
		".agents/context/templates/project/sot.md": {
			// Constraint routing and discovery states.
			"| Architectural constraints |",
			"Never keep a second copy",
			"genuinely absent",
			"discovery has not happened yet",
		},
		".agents/context/templates/project/adr.md": {
			// Register/record lifecycle agreement.
			"initially in the Proposed state",
			"update the record and its row together",
			"successor must be accepted before the predecessor is marked Superseded",
			"preserve the predecessor's rationale unchanged",
			"never retires an accepted decision",
			"COMMON DEFAULT, never a COMMON MUST",
		},
		".agents/context/templates/project/decision.md": {
			"initially Proposed",
			"update the register row in the same change",
			"only after the successor is accepted",
			"preserve the predecessor's rationale unchanged",
			"COMMON DEFAULT, never a COMMON MUST",
		},
	}
	for path, wants := range anchors {
		c, ok := byPath[path]
		if !ok {
			t.Errorf("%s not rendered", path)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(c, want) {
				t.Errorf("%s lacks anchor %q", path, want)
			}
		}
	}
}

// TestGuidesAreIndependentlyUsable guards the schema-4 entry points: the
// root guide routes to the common guide and to the managed project region,
// the common guide routes to its topics, and every relative Markdown link in
// a guide resolves against the guide's own directory.
func TestGuidesAreIndependentlyUsable(t *testing.T) {
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		byPath := map[string][]byte{}
		dirs := map[string]bool{}
		for _, f := range s.Files {
			byPath[f.Path] = f.Content
			for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
				dirs[dir] = true
			}
		}

		// The root guide is language-independent: it renders identically for
		// every mode and never branches on the selected languages.
		if m.name == "go" {
			rootGuide = byPath["AGENTS.md"]
		} else if !bytes.Equal(rootGuide, byPath["AGENTS.md"]) {
			t.Errorf("%s: AGENTS.md differs from the go-only rendering", m.name)
		}

		for _, guide := range []string{"AGENTS.md", ".agents/context/AGENTS.md"} {
			c := byPath[guide]
			if c == nil {
				t.Fatalf("%s: %s not rendered", m.name, guide)
			}
			for _, target := range relativeMarkdownLinks(string(c)) {
				dir := path.Dir(guide)
				resolved := path.Clean(path.Join(dir, target))
				if byPath[resolved] == nil && !dirs[strings.TrimSuffix(resolved, "/")] {
					t.Errorf("%s/%s: link target %q does not resolve to a scaffolded file (from %s)", m.name, guide, target, resolved)
				}
			}
		}

		// The project guide template links its sibling documents: its
		// relative references must resolve against an instantiated project
		// context, which holds exactly these files.
		guideDir := map[string]bool{}
		for _, name := range []string{"AGENTS.md", "adr.md", "sot.md", "status.md", "tools.md"} {
			guideDir[name] = true
		}
		for _, target := range relativeMarkdownLinks(string(byPath[".agents/context/templates/project/AGENTS.md"])) {
			if !guideDir[path.Clean(target)] {
				t.Errorf("%s: the project guide template links %q, which a project context will not contain", m.name, target)
			}
		}

		// The common guide routes to mandatory policy; the root guide routes
		// to the common guide and the managed project region.
		if !bytes.Contains(byPath[".agents/context/AGENTS.md"], []byte("policy.md")) {
			t.Errorf("%s: the common guide does not route to policy.md", m.name)
		}
		if !bytes.Contains(byPath["AGENTS.md"], []byte(".agents/context/AGENTS.md")) {
			t.Errorf("%s: the root guide does not route to the common guide", m.name)
		}
	}
}

var rootGuide []byte

// relativeMarkdownLinks returns the targets of Markdown links and images
// that are relative URLs (no scheme, no leading slash and no fragment).
func relativeMarkdownLinks(s string) []string {
	var out []string
	for _, m := range linkPattern.FindAllStringSubmatch(s, -1) {
		target := m[1]
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "/") ||
			strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i]
		}
		if target != "" {
			out = append(out, target)
		}
	}
	return out
}

var linkPattern = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// TestContextRootRouter guards the schema-4 routing structure: the context
// root carries the common AGENTS.md guide, the shared issue template exists,
// the project guide template exists, and no empty project-container
// directory is generated.
func TestContextRootRouter(t *testing.T) {
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, f := range s.Files {
			paths = append(paths, f.Path)
		}
		for _, want := range []string{
			".agents/context/AGENTS.md",
			".agents/context/templates/issue.md",
			".agents/context/templates/project/AGENTS.md",
			".agents/context/templates/project/adr.md",
			".agents/context/templates/project/decision.md",
			".agents/context/templates/project/sot.md",
		} {
			if !slices.Contains(paths, want) {
				t.Errorf("%s: %s not rendered", m.name, want)
			}
		}
		if slices.Contains(paths, ".agents/context/projects/.gitkeep") {
			t.Errorf("%s: generated the obsolete .agents/context/projects container", m.name)
		}
	}
}

// TestNoGlobalProvisioning guards the GLOBAL boundary: GLOBAL preferences
// are external to the workspace, so the scaffold must never reference a
// location in the user's home, name a personal directory, or instruct
// agents to read or write one.
func TestNoGlobalProvisioning(t *testing.T) {
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range s.Files {
			for _, banned := range []string{"~/.agents", "$HOME", "~/", "home directory"} {
				if bytes.Contains(f.Content, []byte(banned)) {
					t.Errorf("%s/%s references the personal location %q; GLOBAL is external to the workspace", m.name, f.Path, banned)
				}
			}
		}
	}
}

func TestAgentsRoutingMarkers(t *testing.T) {
	s, err := Render(Options{Go: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Files {
		if f.Path != "AGENTS.md" {
			continue
		}
		c := string(f.Content)
		start := strings.Index(c, "<!-- za:projects:start -->")
		end := strings.Index(c, "<!-- za:projects:end -->")
		if start < 0 || end < start || strings.Count(c, "za:projects:start") != 1 || strings.Count(c, "za:projects:end") != 1 {
			t.Fatalf("AGENTS.md lacks a single well-formed routing region")
		}
		return
	}
	t.Fatal("AGENTS.md not rendered")
}

func TestWriteNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Render(Options{Go: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write(root); err == nil {
		t.Fatal("Write overwrote an existing file")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(data) != "user" {
		t.Errorf("existing file modified: %q", data)
	}
}

func TestTopLevel(t *testing.T) {
	s, err := Render(Options{Go: true, Python: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".agents", ".editorconfig", ".gitattributes", ".gitignore", ".worktrees", "AGENTS.md", "CLAUDE.md", "za.toml"}
	if got := s.TopLevel(); !slices.Equal(got, want) {
		t.Errorf("TopLevel() = %v, want %v", got, want)
	}
}

func TestManifest(t *testing.T) {
	want := map[string]string{
		"go":       "schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n",
		"python":   "schema = 4\n\n[workspace]\ngo = false\npython = true\nrust = false\n",
		"rust":     "schema = 4\n\n[workspace]\ngo = false\npython = false\nrust = true\n",
		"polyglot": "schema = 4\n\n[workspace]\ngo = true\npython = true\nrust = false\n",
		"all":      "schema = 4\n\n[workspace]\ngo = true\npython = true\nrust = true\n",
	}
	for _, m := range modes {
		s, err := Render(m.opts)
		if err != nil {
			t.Fatal(err)
		}
		var manifests []string
		for _, f := range s.Files {
			if path.Base(f.Path) != "za.toml" {
				continue
			}
			manifests = append(manifests, f.Path)
			if got := string(f.Content); got != want[m.name] {
				t.Errorf("%s: za.toml =\n%s\nwant\n%s", m.name, got, want[m.name])
			}
		}
		// Exactly one manifest, at the workspace root and never under .agents/.
		if !slices.Equal(manifests, []string{"za.toml"}) {
			t.Errorf("%s: manifests rendered at %v, want only za.toml", m.name, manifests)
		}
	}
}

// TestAllLanguageCombinations covers every non-empty flag combination of
// the "za init" interface: exact capability booleans, and the root Cargo
// manifest only for Rust-enabled workspaces.
func TestAllLanguageCombinations(t *testing.T) {
	tests := []struct {
		go_, python, rust bool
	}{
		{true, false, false},
		{false, true, false},
		{false, false, true},
		{true, true, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("go=%t,python=%t,rust=%t", tt.go_, tt.python, tt.rust)
		t.Run(name, func(t *testing.T) {
			s, err := Render(Options{Go: tt.go_, Python: tt.python, Rust: tt.rust})
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string]string{}
			for _, f := range s.Files {
				byPath[f.Path] = string(f.Content)
			}
			wantManifest := fmt.Sprintf("schema = 4\n\n[workspace]\ngo = %t\npython = %t\nrust = %t\n", tt.go_, tt.python, tt.rust)
			if got := byPath["za.toml"]; got != wantManifest {
				t.Errorf("za.toml =\n%s\nwant\n%s", got, wantManifest)
			}
			wantCargo := "[workspace]\nmembers = []\nexclude = [\".worktrees\"]\nresolver = \"3\"\n"
			if tt.rust {
				if got := byPath["Cargo.toml"]; got != wantCargo {
					t.Errorf("Cargo.toml =\n%s\nwant\n%s", got, wantCargo)
				}
			} else if got, ok := byPath["Cargo.toml"]; ok {
				t.Errorf("Cargo.toml rendered without --rust:\n%s", got)
			}
		})
	}
}

// TestRustScaffold guards the Rust bootstrap contract: a virtual workspace
// manifest and no dummy package, source directory or toolchain file; Rust
// integration constraints and ignore rules only for Rust-enabled workspaces.
func TestRustScaffold(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		present []string
		absent  []string
	}{
		{
			name:    "rust only",
			opts:    Options{Rust: true},
			present: []string{"Cargo.toml"},
			absent:  []string{"rust-toolchain.toml", "rust-toolchain", "src", "go.work", "pyproject.toml"},
		},
		{
			name:   "go and python without rust",
			opts:   Options{Go: true, Python: true},
			absent: []string{"Cargo.toml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Render(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string][]byte{}
			for _, f := range s.Files {
				byPath[f.Path] = f.Content
			}
			for _, p := range tt.present {
				if _, ok := byPath[p]; !ok {
					t.Errorf("missing %s", p)
				}
			}
			for _, p := range tt.absent {
				if _, ok := byPath[p]; ok {
					t.Errorf("za must not generate %s", p)
				}
			}

			if cargo, ok := byPath["Cargo.toml"]; ok {
				if bytes.Contains(cargo, []byte("[package]")) {
					t.Error("root Cargo.toml must stay a virtual manifest without [package]")
				}
				if !bytes.Contains(cargo, []byte(`members = []`)) || !bytes.Contains(cargo, []byte(`exclude = [".worktrees"]`)) || !bytes.Contains(cargo, []byte(`resolver = "3"`)) {
					t.Errorf("Cargo.toml lacks the required virtual workspace structure:\n%s", cargo)
				}
			}

			// The common tooling guide documents integration behavior for the
			// enabled capabilities only: the cargo row and the Rust constraint
			// appear exactly when Rust is selected, and never mention the
			// removed per-language manuals.
			tooling := byPath[".agents/context/tooling.md"]
			if tooling == nil {
				t.Fatalf(".agents/context/tooling.md not rendered")
			}
			if mentions := bytes.Contains(tooling, []byte("`cargo`/`rustc`")); mentions != tt.opts.Rust {
				t.Errorf("tooling.md cargo row = %t, want %t:\n%s", mentions, tt.opts.Rust, tooling)
			}
			if mentions := bytes.Contains(tooling, []byte("cargo metadata")); mentions != tt.opts.Rust {
				t.Errorf("tooling.md cargo metadata constraint = %t, want %t", mentions, tt.opts.Rust)
			}
			for _, capRow := range []string{"`go` | Go toolchain", "`uv` | Python project manager"} {
				if want := tt.opts.Go || strings.HasPrefix(capRow, "`uv`") && tt.opts.Python; strings.Contains(string(tooling), capRow) != want {
					t.Errorf("tooling.md row %q presence = %t, want %t", capRow, strings.Contains(string(tooling), capRow), want)
				}
			}

			// The target directory is ignored for Rust workspaces only, and
			// a root Cargo.lock is never ignored.
			gitignore := string(byPath[".gitignore"])
			if strings.Contains(gitignore, "/target/") != tt.opts.Rust {
				t.Errorf(".gitignore /target/ rule = %t, want %t:\n%s", strings.Contains(gitignore, "/target/"), tt.opts.Rust, gitignore)
			}
			if strings.Contains(gitignore, "Cargo.lock") {
				t.Errorf(".gitignore must not ignore Cargo.lock:\n%s", gitignore)
			}
		})
	}
}
