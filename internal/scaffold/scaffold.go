// Package scaffold renders the za-managed files of a new meta-workspace from
// embedded templates.
//
// Rendering is uniform: every file under templates/ carries the ".tmpl"
// suffix, which the tmpl package removes in the output. The suffix keeps
// files such as AGENTS.md, CLAUDE.md and .gitignore inert inside this
// repository.
//
// The rendered za.toml is the workspace manifest. Its "schema" key is the
// version of the workspace layout, independent of the za release version.
package scaffold

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tnotstar/go-za/internal/tmpl"
)

//go:embed all:templates
var templates embed.FS

const templateRoot = "templates"

// Options selects the workspace types to scaffold.
type Options struct {
	Go     bool
	Python bool
	Rust   bool
}

// File is one rendered file. Path is slash-separated and relative to the
// workspace root.
type File = tmpl.File

// Scaffold is the complete set of za-managed workspace content.
type Scaffold struct {
	Files []File
	// Dirs are directories created empty. They are ignored by Git, so they
	// carry no .gitkeep.
	Dirs []string
}

// conditional lists files rendered only for some workspace types. Language
// integration facts are not separate files: the common tooling guide carries
// them conditionally for the enabled capabilities.
var conditional = map[string]func(Options) bool{
	"Cargo.toml": func(o Options) bool { return o.Rust },
}

var ignoredDirs = []string{".agents/runtime", ".worktrees"}

// Render renders the scaffold for opts deterministically. Rendering itself
// is delegated to the tmpl package; scaffold only decides which rendered
// files belong to the workspace (conditional per-language content) and which
// directories are created empty.
func Render(opts Options) (Scaffold, error) {
	s := Scaffold{Dirs: slices.Clone(ignoredDirs)}
	files, err := tmpl.Render(templates, templateRoot, opts)
	if err != nil {
		return Scaffold{}, err
	}
	for _, f := range files {
		if include, ok := conditional[f.Path]; ok && !include(opts) {
			continue
		}
		s.Files = append(s.Files, f)
	}
	return s, nil
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
