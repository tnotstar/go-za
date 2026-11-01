package scaffold

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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

// Hard size ceilings from the architecture's context budget.
var ceilings = map[string]int{
	"AGENTS.md":                                            4 << 10,
	".agents/context/INDEX.md":                             3 << 10,
	".agents/context/templates/issue.md":                   12 << 10,
	".agents/context/templates/project/INDEX.md":           3 << 10,
	".agents/context/templates/project/tools.md":           6 << 10,
	".agents/context/templates/project/constraints.md":     6 << 10,
	".agents/context/templates/project/source-of-truth.md": 6 << 10,
	".agents/context/templates/project/status.md":          2 << 10,
	".agents/context/templates/project/adr.md":             6 << 10,
}

func TestContextSizeBudgets(t *testing.T) {
	s, err := Render(Options{Go: true, Python: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Files {
		limit, ok := ceilings[f.Path]
		if !ok && strings.HasPrefix(f.Path, ".agents/context/") && !strings.Contains(f.Path, "/templates/") {
			limit, ok = 8<<10, true
		}
		if ok && len(f.Content) > limit {
			t.Errorf("%s is %d bytes, exceeds ceiling of %d", f.Path, len(f.Content), limit)
		}
	}
}

func TestRenderedContentInvariants(t *testing.T) {
	obsolete := []string{".agents/context/_global/", ".agents/context/projects/"}
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
		}
	}
}

// TestContextRootRouter guards the schema-3 routing structure: the context
// root carries an INDEX.md router, the shared issue template exists, and no
// empty project-container directory is generated.
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
			".agents/context/INDEX.md",
			".agents/context/templates/issue.md",
			".agents/context/templates/project/INDEX.md",
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
		"go":       "schema = 3\n\n[workspace]\ngo = true\npython = false\nrust = false\n",
		"python":   "schema = 3\n\n[workspace]\ngo = false\npython = true\nrust = false\n",
		"rust":     "schema = 3\n\n[workspace]\ngo = false\npython = false\nrust = true\n",
		"polyglot": "schema = 3\n\n[workspace]\ngo = true\npython = true\nrust = false\n",
		"all":      "schema = 3\n\n[workspace]\ngo = true\npython = true\nrust = true\n",
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
			wantManifest := fmt.Sprintf("schema = 3\n\n[workspace]\ngo = %t\npython = %t\nrust = %t\n", tt.go_, tt.python, tt.rust)
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
// documents, routing and ignore rules only for Rust-enabled workspaces.
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
			present: []string{"Cargo.toml", ".agents/context/rust-standards.md"},
			absent:  []string{"rust-toolchain.toml", "rust-toolchain", "src", ".agents/context/go-standards.md", ".agents/context/python-standards.md"},
		},
		{
			name:   "go and python without rust",
			opts:   Options{Go: true, Python: true},
			absent: []string{"Cargo.toml", ".agents/context/rust-standards.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Render(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string][]byte{}
			var paths []string
			for _, f := range s.Files {
				byPath[f.Path] = f.Content
				paths = append(paths, f.Path)
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

			// Rust routing exists exactly when Rust is selected, in both
			// routers, and no router mentions rust-standards otherwise.
			for _, router := range []string{"AGENTS.md", ".agents/context/INDEX.md"} {
				c := byPath[router]
				if c == nil {
					t.Fatalf("%s not rendered", router)
				}
				if mentions := bytes.Contains(c, []byte("rust-standards")); mentions != tt.opts.Rust {
					t.Errorf("%s rust-standards mention = %t, want %t:\n%s", router, mentions, tt.opts.Rust, c)
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
