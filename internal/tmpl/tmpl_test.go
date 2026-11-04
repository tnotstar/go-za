package tmpl

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

type data struct {
	Go bool
}

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"templates/AGENTS.md.tmpl":            {Data: []byte("# Guide {{if .Go}}go{{else}}other{{end}}\n")},
		"templates/CLAUDE.md.tmpl":            {Data: []byte("Read AGENTS.md.\n")},
		"templates/.agents/context/x.md.tmpl": {Data: []byte("nested\n")},
		"templates/.agents/context/y.md.tmpl": {Data: []byte("nested too\n")},
		"templates/.agents/hooks/meta/d.tmpl": {Data: []byte("deep\n")},
		"templates/.gitignore.tmpl":           {Data: []byte("/ignored/\n")},
		"templates/za.toml.tmpl":              {Data: []byte("go = {{.Go}}\n")},
		"templates/not-a-template.txt":        {Data: []byte("stray\n")},
	}
}

func TestRenderStripsSuffixUniformly(t *testing.T) {
	f := testFS()
	// Every file must carry the suffix, including the stray one, so the
	// fixture only proves the rule once the stray file is fixed.
	f["templates/not-a-template.txt.tmpl"] = f["templates/not-a-template.txt"]
	delete(f, "templates/not-a-template.txt")

	files, err := Render(f, "templates", data{Go: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got []string
	byPath := map[string]string{}
	for _, file := range files {
		got = append(got, file.Path)
		byPath[file.Path] = string(file.Content)
	}
	want := []string{
		".agents/context/x.md",
		".agents/context/y.md",
		".agents/hooks/meta/d",
		".gitignore",
		"AGENTS.md",
		"CLAUDE.md",
		"not-a-template.txt",
		"za.toml",
	}
	if !slices.Equal(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if byPath["AGENTS.md"] != "# Guide go\n" {
		t.Errorf("AGENTS.md = %q", byPath["AGENTS.md"])
	}
	if byPath["za.toml"] != "go = true\n" {
		t.Errorf("za.toml = %q", byPath["za.toml"])
	}
}

func TestRenderRequiresSuffixOnEveryFile(t *testing.T) {
	f := testFS()
	_, err := Render(f, "templates", data{})
	if err == nil || !strings.Contains(err.Error(), "templates/not-a-template.txt lacks the .tmpl suffix") {
		t.Fatalf("err = %v, want the suffix error for the stray file", err)
	}
}

func TestRenderMissingKeyIsAnError(t *testing.T) {
	f := fstest.MapFS{"templates/x.md.tmpl": {Data: []byte("{{.Python}}\n")}}
	_, err := Render(f, "templates", data{Go: true})
	if err == nil {
		t.Fatal("Render succeeded although the template references an absent field")
	}
}

func TestRenderRejectsUnparseableTemplate(t *testing.T) {
	f := fstest.MapFS{"templates/x.md.tmpl": {Data: []byte("{{if}}\n")}}
	if _, err := Render(f, "templates", data{}); err == nil {
		t.Fatal("Render succeeded although the template does not parse")
	}
}

func TestRenderWalkOrderIsDeterministic(t *testing.T) {
	f := fstest.MapFS{
		"templates/z.tmpl":   {Data: []byte("z\n")},
		"templates/a/b.tmpl": {Data: []byte("a/b\n")},
		"templates/a.tmpl":   {Data: []byte("a\n")},
	}
	// fs.WalkDir order: depth-first over lexically sorted entries, so the
	// nested a/b precedes its sibling a.tmpl. Pinning the exact order guards
	// the deterministic rendering the scaffold's file list relies on.
	want := []string{"a/b", "a", "z"}
	for range 2 {
		files, err := Render(f, "templates", nil)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		var got []string
		for _, file := range files {
			got = append(got, file.Path)
		}
		if !slices.Equal(got, want) {
			t.Errorf("paths = %v, want %v", got, want)
		}
	}
}

// TestRenderRootPrefixIsStrippedExact guards the path mapping: only the
// exact "root/" prefix is removed, so a root of "templates" must not mangle
// an unrelated prefix such as "templates-extra/".
func TestRenderRootPrefixIsStrippedExact(t *testing.T) {
	f := fstest.MapFS{
		"templates/a.tmpl":       {Data: []byte("a\n")},
		"templates-extra/a.tmpl": {Data: []byte("outside\n")},
	}
	files, err := Render(f, "templates", nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got []string
	for _, file := range files {
		got = append(got, file.Path)
	}
	if !slices.Equal(got, []string{"a"}) {
		t.Errorf("paths = %v, want only [a]", got)
	}
}
