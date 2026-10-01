// Package scaffold renders the za-managed files of a new meta-workspace from
// embedded templates.
//
// Every file under templates/ carries a ".tmpl" suffix, which is removed in
// the output. The suffix keeps files such as AGENTS.md, CLAUDE.md and
// .gitignore inert inside this repository.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
)

//go:embed all:templates
var templates embed.FS

const (
	templateRoot = "templates"
	templateExt  = ".tmpl"
)

// Options selects the workspace types to scaffold.
type Options struct {
	Go     bool
	Python bool
}

// File is one rendered file. Path is slash-separated and relative to the
// workspace root.
type File struct {
	Path    string
	Content []byte
}

// Scaffold is the complete set of za-managed workspace content.
type Scaffold struct {
	Files []File
	// Dirs are directories created empty. They are ignored by Git, so they
	// carry no .gitkeep.
	Dirs []string
}

// conditional lists files rendered only for some workspace types.
var conditional = map[string]func(Options) bool{
	".agents/context/_global/go-standards.md":     func(o Options) bool { return o.Go },
	".agents/context/_global/python-standards.md": func(o Options) bool { return o.Python },
}

var ignoredDirs = []string{".agents/runtime", ".worktrees"}

// Render renders the scaffold for opts deterministically.
func Render(opts Options) (Scaffold, error) {
	s := Scaffold{Dirs: slices.Clone(ignoredDirs)}
	err := fs.WalkDir(templates, templateRoot, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(name, templateExt) {
			return fmt.Errorf("template %s lacks the %s suffix", name, templateExt)
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, templateRoot+"/"), templateExt)
		if include, ok := conditional[rel]; ok && !include(opts) {
			return nil
		}

		content, err := render(name, opts)
		if err != nil {
			return err
		}
		s.Files = append(s.Files, File{Path: rel, Content: content})
		return nil
	})
	if err != nil {
		return Scaffold{}, fmt.Errorf("render scaffold: %w", err)
	}
	return s, nil
}

func render(name string, opts Options) ([]byte, error) {
	src, err := templates.ReadFile(name)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TopLevel returns the sorted, unique top-level entry names s creates.
func (s Scaffold) TopLevel() []string {
	var names []string
	for _, f := range s.Files {
		names = append(names, firstSegment(f.Path))
	}
	for _, d := range s.Dirs {
		names = append(names, firstSegment(d))
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func firstSegment(p string) string {
	first, _, _ := strings.Cut(path.Clean(p), "/")
	return first
}

// Write creates the scaffold under root. It never overwrites existing files.
func (s Scaffold) Write(root string) error {
	for _, f := range s.Files {
		dst := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", f.Path, err)
		}
		if err := writeNew(dst, f.Content); err != nil {
			return fmt.Errorf("write %s: %w", f.Path, err)
		}
	}
	for _, d := range s.Dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	return nil
}

func writeNew(name string, content []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(content)
	return errors.Join(werr, f.Close())
}
