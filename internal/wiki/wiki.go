// Package wiki manages the workspace wiki pages of the .za/ layer: flat,
// organically named Markdown files under .za/wiki/ — the private source of
// truth for observable behavior, architecture and domain knowledge.
//
// Pages are located through the same upward za.toml discovery the other
// commands use; a workspace without .za/wiki/ is rejected fail-closed.
// Slugs are validated strictly (no empty, dot-prefixed, separator-bearing
// or traversal names; the img/ asset directory is reserved), and content
// carrying CR line endings is an error, matching the repository's LF-only
// convention.
package wiki

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/tnotstar/go-za/internal/manifest"
)

// Dir is the workspace-relative directory holding the wiki pages.
const Dir = ".za/wiki"

// TemplatePath is the workspace-relative path of the canonical wiki page
// template used by "za wiki new".
const TemplatePath = ".za/templates/wiki.md"

// AssetsDir is the reserved subdirectory of Dir for page assets.
const AssetsDir = "img"

// Page is one wiki page summary as rendered by "za wiki ls".
type Page struct {
	Slug  string // file name without the .md suffix
	Title string // first "# " heading, or the slug as fallback
}

var wikiDirError = fmt.Errorf("%s is missing in the workspace; run \"za init\" or scaffold the .za/ layer first", Dir)

// Root returns the workspace root enclosing start for wiki operations,
// rejecting an invalid or unsupported manifest and a workspace whose
// .za/wiki/ directory is missing.
func Root(start string) (string, error) {
	root, err := manifest.Find(start)
	if err != nil {
		return "", err
	}
	if _, err := manifest.Load(root); err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(Dir)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", wikiDirError
	case err != nil:
		return "", fmt.Errorf("inspect %s: %w", filepath.Join(root, Dir), err)
	case !info.IsDir():
		return "", fmt.Errorf("%s is not a directory in the workspace at %s", Dir, root)
	}
	return root, nil
}

// Path returns the workspace-relative path of the page for slug.
func Path(slug string) string {
	return path.Join(Dir, slug+".md")
}

// NormalizeSlug validates a page slug. It accepts the name with or without
// a trailing ".md" and returns the bare slug. Rejected fail-closed: empty
// names, dot-prefixed names, path separators, "..", carriage returns or
// other control characters, and reserved directory names (the asset
// directory img, case-insensitively).
func NormalizeSlug(s string) (string, error) {
	slug := strings.TrimSuffix(s, ".md")
	switch {
	case slug == "":
		return "", fmt.Errorf("empty wiki page name")
	case strings.ContainsAny(slug, "/\\"):
		return "", fmt.Errorf("%q contains a path separator; wiki pages live flat in %s", s, Dir)
	}
	for _, r := range slug {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%q contains a control character; page names are plain LF-only slugs", s)
		}
	}
	switch {
	case slug == ".." || strings.HasPrefix(slug, "../"):
		return "", fmt.Errorf("%q escapes %s", s, Dir)
	case strings.HasPrefix(slug, "."):
		return "", fmt.Errorf("%q is a hidden name; wiki page names must not start with a dot", s)
	}
	if strings.EqualFold(slug, AssetsDir) {
		return "", fmt.Errorf("%q is reserved for the %s/%s asset directory", s, Dir, AssetsDir)
	}
	return slug, nil
}

// New creates .za/wiki/<slug>.md exclusively: it never overwrites an
// existing page or directory. The body comes from the workspace template
// (.za/templates/wiki.md, its "# " heading replaced with the title) and
// falls back to the built-in draft skeleton. An empty title is derived
// from the slug. New returns the created page's workspace-relative path.
func New(root, slug, title string) (string, error) {
	slug, err := NormalizeSlug(slug)
	if err != nil {
		return "", err
	}
	if title == "" {
		title = TitleFromSlug(slug)
	}
	body, err := bodyFromTemplate(root, title)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, filepath.FromSlash(Path(slug)))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s is missing in the workspace; run \"za init\" or scaffold the .za/ layer first", Dir)
		}
		return "", fmt.Errorf("create %s: %w", Path(slug), err)
	}
	if _, werr := f.WriteString(body); werr != nil {
		return "", errors.Join(werr, f.Close())
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return Path(slug), nil
}

// bodyFromTemplate renders the page body: the workspace template's shape
// with its first "# " heading replaced by the title, or the built-in draft
// skeleton when the workspace carries no template.
func bodyFromTemplate(root, title string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(TemplatePath)))
	if errors.Is(err, fs.ErrNotExist) {
		return defaultBody(title), nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", TemplatePath, err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if line != "" && strings.HasSuffix(line, "\r") {
			return "", fmt.Errorf("%s ends with a carriage return; templates are LF-only", TemplatePath)
		}
		if strings.HasPrefix(line, "# ") {
			lines[i] = "# " + title
			return strings.Join(lines, "\n"), nil
		}
	}
	// A template without a heading still gets one, ahead of its content.
	return "# " + title + "\n" + strings.Join(lines, "\n"), nil
}

// defaultBody is the built-in draft skeleton for pages created without a
// workspace template.
func defaultBody(title string) string {
	return "# " + title + "\n\nSummary: <one line: what this page specifies and for which project or scope.>\n\nStatus: draft\n"
}

// TitleFromSlug derives a readable default title from a slug by splitting
// on hyphens and capitalizing each word: go-minolas-architecture becomes
// "Go Minolas Architecture".
func TitleFromSlug(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		words[i] = string([]rune{toUpperASCII(r[0])}) + string(r[1:])
	}
	return strings.Join(words, " ")
}

func toUpperASCII(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r
}

// Ls scans .za/wiki/ and returns the pages sorted lexicographically by
// slug. Only regular .md files directly in the directory are listed;
// subdirectories such as img/ and any other entries are skipped. Titles
// come from each page's first "# " heading, read through a bounded stream
// so large pages are never loaded whole; a page without a heading falls
// back to its slug.
func Ls(root string) ([]Page, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", Dir, err)
	}
	var pages []Page
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		if !de.Type().IsRegular() {
			return nil, fmt.Errorf("%s/%s is not a regular file", Dir, de.Name())
		}
		slug := strings.TrimSuffix(de.Name(), ".md")
		title, err := firstHeading(filepath.Join(dir, de.Name()), slug)
		if err != nil {
			return nil, err
		}
		pages = append(pages, Page{Slug: slug, Title: title})
	}
	return pages, nil
}

// maxHeadingScan bounds the streamed prefix searched for a heading, so a
// large page without headings costs a bounded read, never its full size.
const maxHeadingScan = 64 << 10

// firstHeading streams the page until the first "# " heading line and
// returns its text (the slug as fallback). CR line endings fail closed.
func firstHeading(p, fallback string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()

	r := bufio.NewReader(io.LimitReader(f, maxHeadingScan))
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read %s: %w", p, err)
		}
		text := strings.TrimSuffix(line, "\n")
		if strings.HasSuffix(text, "\r") && text != "\r" {
			return "", fmt.Errorf("%s ends a line with a carriage return; pages are LF-only", p)
		}
		text = strings.TrimSuffix(text, "\r")
		if strings.HasPrefix(text, "# ") {
			if title := strings.TrimSpace(text[2:]); title != "" {
				return title, nil
			}
		}
		if err == io.EOF {
			return fallback, nil
		}
	}
}

// Read returns the full content of the page for slug.
func Read(root, slug string) ([]byte, error) {
	slug, err := NormalizeSlug(slug)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(Path(slug))))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("wiki page %s does not exist (%s)", slug, Path(slug))
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Path(slug), err)
	}
	return data, nil
}
