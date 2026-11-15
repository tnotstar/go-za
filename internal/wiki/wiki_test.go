package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wikiFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestNormalizeSlug(t *testing.T) {
	valid := map[string]string{
		"go-minolas-architecture":    "go-minolas-architecture",
		"go-minolas-architecture.md": "go-minolas-architecture",
		"index":                      "index",
		"weird_but_legal.page.name":  "weird_but_legal.page.name",
		"UPPER-case":                 "UPPER-case",
	}
	for in, want := range valid {
		got, err := NormalizeSlug(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSlug(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := map[string]string{
		"":            "empty wiki page name",
		".md":         "empty wiki page name",
		".hidden":     "hidden name",
		"..":          "escapes",
		"../escape":   "path separator",
		"a/b":         "path separator",
		`a\b`:         "path separator",
		"img":         "reserved",
		"IMG":         "reserved",
		"line\rbreak": "control character",
		"tab\tstop":   "control character",
	}
	for in, want := range invalid {
		if got, err := NormalizeSlug(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("NormalizeSlug(%q) = %q, %v; want error containing %q", in, got, err, want)
		}
	}
}

func TestTitleFromSlug(t *testing.T) {
	tests := map[string]string{
		"go-minolas-architecture": "Go Minolas Architecture",
		"index":                   "Index",
		"a--b":                    "A  B",
	}
	for slug, want := range tests {
		if got := TitleFromSlug(slug); got != want {
			t.Errorf("TitleFromSlug(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestNew(t *testing.T) {
	t.Run("falls back to the draft skeleton and derives the title", func(t *testing.T) {
		root := wikiFixture(t, nil)
		p, err := New(root, "go-minolas-architecture", "")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if p != ".za/wiki/go-minolas-architecture.md" {
			t.Errorf("New path = %q", p)
		}
		data, err := os.ReadFile(filepath.Join(root, Dir, "go-minolas-architecture.md"))
		if err != nil {
			t.Fatal(err)
		}
		want := "# Go Minolas Architecture\n\nSummary: <one line: what this page specifies and for which project or scope.>\n\nStatus: draft\n"
		if string(data) != want {
			t.Errorf("page =\n%q\nwant\n%q", data, want)
		}
	})
	t.Run("uses the workspace template and replaces the heading", func(t *testing.T) {
		template := "# <Page Title>\n\nSummary: <one line.>\n\nStatus: <draft | accepted>\n"
		root := wikiFixture(t, map[string]string{TemplatePath: template})
		p, err := New(root, "spec-page.md", "A Clean Title")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if p != ".za/wiki/spec-page.md" {
			t.Errorf("New path = %q", p)
		}
		data, _ := os.ReadFile(filepath.Join(root, Dir, "spec-page.md"))
		want := "# A Clean Title\n\nSummary: <one line.>\n\nStatus: <draft | accepted>\n"
		if string(data) != want {
			t.Errorf("page =\n%q\nwant\n%q", data, want)
		}
	})
	t.Run("never overwrites", func(t *testing.T) {
		root := wikiFixture(t, map[string]string{".za/wiki/taken.md": "# Original\n"})
		if p, err := New(root, "taken", "Other"); err == nil {
			t.Errorf("New overwrote an existing page: %q", p)
		}
		data, _ := os.ReadFile(filepath.Join(root, Dir, "taken.md"))
		if string(data) != "# Original\n" {
			t.Errorf("existing page modified: %q", data)
		}
	})
	t.Run("rejects invalid slugs", func(t *testing.T) {
		root := wikiFixture(t, nil)
		for _, slug := range []string{"", "img", ".hidden", "a/b", ".."} {
			if p, err := New(root, slug, "T"); err == nil {
				t.Errorf("New(%q) = %q, want an error", slug, p)
			}
		}
	})
	t.Run("template with CR fails closed", func(t *testing.T) {
		root := wikiFixture(t, map[string]string{TemplatePath: "# T\r\n\r\nbody\r\n"})
		if p, err := New(root, "cr-page", "T"); err == nil || !strings.Contains(err.Error(), "carriage return") {
			t.Errorf("New with a CR template = %q, %v; want a carriage-return error", p, err)
		}
	})
}

func TestLs(t *testing.T) {
	root := wikiFixture(t, map[string]string{
		".za/wiki/zz-page.md":      "# Last Page\n\nbody\n",
		".za/wiki/aa-first.md":     "no heading at all, falls back to the slug\n",
		".za/wiki/empty.md":        "",
		".za/wiki/index.md":        "# Workspace Wiki Index\n\nSummary: ...\n",
		".za/wiki/img/picture.png": "not a page",
		".za/wiki/notes.txt":       "not a page either",
	})
	pages, err := Ls(root)
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}
	var slugs []string
	for _, p := range pages {
		slugs = append(slugs, p.Slug)
	}
	want := []string{"aa-first", "empty", "index", "zz-page"}
	if strings.Join(slugs, ",") != strings.Join(want, ",") {
		t.Errorf("slugs = %v, want %v", slugs, want)
	}
	titles := map[string]string{}
	for _, p := range pages {
		titles[p.Slug] = p.Title
	}
	if titles["zz-page"] != "Last Page" || titles["aa-first"] != "aa-first" || titles["index"] != "Workspace Wiki Index" || titles["empty"] != "empty" {
		t.Errorf("unexpected titles: %v", titles)
	}
}

func TestLsRejectsCarriageReturns(t *testing.T) {
	root := wikiFixture(t, map[string]string{".za/wiki/cr.md": "# Title\r\n\r\nbody\r\n"})
	if _, err := Ls(root); err == nil || !strings.Contains(err.Error(), "carriage return") {
		t.Errorf("Ls with a CR page = error %v, want a carriage-return error", err)
	}
}

func TestRead(t *testing.T) {
	root := wikiFixture(t, map[string]string{".za/wiki/page.md": "# Page\n\nbody\n"})
	for _, in := range []string{"page", "page.md"} {
		data, err := Read(root, in)
		if err != nil || string(data) != "# Page\n\nbody\n" {
			t.Errorf("Read(%q) = %q, %v", in, data, err)
		}
	}
	if _, err := Read(root, "missing"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Read(missing) = error %v, want does not exist", err)
	}
	if _, err := Read(root, "../escape"); err == nil {
		t.Error("Read accepted a traversal slug")
	}
}

func TestRoot(t *testing.T) {
	t.Run("discovers the workspace", func(t *testing.T) {
		root := wikiFixture(t, nil)
		got, err := Root(filepath.Join(root, "deep"))
		if got != root {
			t.Errorf("Root = %q, %v; want %q", got, err, root)
		}
	})
	t.Run("missing .za/wiki fails closed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Root(root); err == nil || !strings.Contains(err.Error(), Dir+" is missing") {
			t.Errorf("Root error = %v, want containing %q", err, Dir+" is missing")
		}
	})
	t.Run("outside a workspace", func(t *testing.T) {
		if _, err := Root(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not inside a za workspace") {
			t.Errorf("Root error = %v, want not inside a za workspace", err)
		}
	})
}
