// Package tmpl renders text templates uniformly.
//
// Every template file carries the ".tmpl" suffix, which is removed in the
// rendered output path. The suffix keeps generated files such as AGENTS.md,
// CLAUDE.md and .gitignore inert inside this repository while staying
// ordinary templates at build time. There are no per-file exceptions: a
// file without the suffix is an error, not a differently named output.
//
// Rendering executes each file as a text/template with missingkey=error, so
// a reference to a field the caller does not provide fails the render
// instead of silently producing an empty string.
package tmpl

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"
)

// Ext is the required suffix of every template file. It is removed in the
// rendered output path.
const Ext = ".tmpl"

// File is one rendered file. Path is slash-separated and relative to root,
// with Ext removed.
type File struct {
	Path    string
	Content []byte
}

// Render walks fsys from root and renders every file with the Ext suffix
// using data, in lexical walk order. A file without the suffix, a template
// that does not parse, or an execution failure aborts the whole render.
func Render(fsys fs.FS, root string, data any) ([]File, error) {
	var out []File
	err := fs.WalkDir(fsys, root, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(name, Ext) {
			return fmt.Errorf("template %s lacks the %s suffix", name, Ext)
		}
		content, err := renderFile(fsys, name, data)
		if err != nil {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, root+"/"), Ext)
		out = append(out, File{Path: rel, Content: content})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("render templates: %w", err)
	}
	return out, nil
}

func renderFile(fsys fs.FS, name string, data any) ([]byte, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
