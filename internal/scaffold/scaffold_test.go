package scaffold

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
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
	{"polyglot", Options{Go: true, Python: true}},
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
	"AGENTS.md": 4 << 10,
	".agents/context/_global/templates/project/INDEX.md":           3 << 10,
	".agents/context/_global/templates/project/tools.md":           6 << 10,
	".agents/context/_global/templates/project/constraints.md":     6 << 10,
	".agents/context/_global/templates/project/source-of-truth.md": 6 << 10,
	".agents/context/_global/templates/project/status.md":          2 << 10,
	".agents/context/_global/templates/project/adr.md":             6 << 10,
}

func TestContextSizeBudgets(t *testing.T) {
	s, err := Render(Options{Go: true, Python: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Files {
		limit, ok := ceilings[f.Path]
		if !ok && strings.HasPrefix(f.Path, ".agents/context/_global/") && !strings.Contains(f.Path, "/templates/") {
			limit, ok = 8<<10, true
		}
		if ok && len(f.Content) > limit {
			t.Errorf("%s is %d bytes, exceeds ceiling of %d", f.Path, len(f.Content), limit)
		}
	}
}

func TestRenderedContentInvariants(t *testing.T) {
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
	want := []string{".agents", ".editorconfig", ".gitattributes", ".gitignore", ".worktrees", "AGENTS.md", "CLAUDE.md"}
	if got := s.TopLevel(); !slices.Equal(got, want) {
		t.Errorf("TopLevel() = %v, want %v", got, want)
	}
}
