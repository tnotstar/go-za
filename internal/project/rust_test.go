package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCargoManifest(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		workspace  bool
		members    []string
		packageSel bool
		crateName  string
		edition    string
		err        string
	}{
		{
			name:       "single root crate",
			content:    "[package]\nname = \"pub\"\nversion = \"0.1.0\"\nedition = \"2021\"\n",
			packageSel: true, crateName: "pub", edition: "2021",
		},
		{
			name:       "crate without edition",
			content:    "[package]\nname = \"pub\"\nversion = \"0.1.0\"\n",
			packageSel: true, crateName: "pub",
		},
		{
			name:      "virtual workspace",
			content:   "[workspace]\nmembers = [\"crates/pub\"]\nresolver = \"2\"\n",
			workspace: true, members: []string{"crates/pub"},
		},
		{
			name:      "composite package and workspace",
			content:   "[package]\nname = \"pub\"\n\n[workspace]\nmembers = []\n",
			workspace: true, members: []string{}, packageSel: true, crateName: "pub",
		},
		{
			name:      "empty workspace table",
			content:   "[workspace]\n",
			workspace: true,
		},
		{
			name:       "package without name",
			content:    "[package]\nversion = \"0.1.0\"\n",
			packageSel: true,
		},
		{
			name: "invalid toml",
			// Two [package] tables: legal TOML only when distinct; the
			// duplicate makes the parser fail.
			content: "[package]\nname = \"a\"\n\n[package]\nname = \"b\"\n",
			err:     "parse TOML",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "Cargo.toml")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := parseCargoManifest(p)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := m.Workspace != nil; got != tt.workspace {
				t.Errorf("workspace = %v, want %v", got, tt.workspace)
			}
			if m.Workspace != nil {
				if strings.Join(m.Workspace.Members, ",") != strings.Join(tt.members, ",") {
					t.Errorf("members = %v, want %v", m.Workspace.Members, tt.members)
				}
			}
			if got := m.Package != nil; got != tt.packageSel {
				t.Fatalf("package = %v, want %v", got, tt.packageSel)
			}
			if m.Package != nil && (m.Package.Name != tt.crateName || m.Package.Edition != tt.edition) {
				t.Errorf("package = %q/%q, want %q/%q", m.Package.Name, m.Package.Edition, tt.crateName, tt.edition)
			}
		})
	}
}

func TestScanNestedManifests(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"Cargo.toml":          "[package]\nname = \"pub\"\n",
		"src/main.rs":         "fn main() {}\n",
		"crates/a/Cargo.toml": "[package]\nname = \"a\"\n",
		"crates/b/Cargo.toml": "[package]\nname = \"b\"\n",
		".git/config":         "[core]\n",
		".git/Cargo.toml":     "ignored inside .git\n",
		"docs/Cargo.toml.md":  "not a manifest\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := scanNestedManifests(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := []string{"crates/a/Cargo.toml", "crates/b/Cargo.toml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("nested = %v, want %v", got, want)
	}
}

func TestAddWorkspaceMember(t *testing.T) {
	tests := []struct {
		name   string
		before string
		id     string
		after  string
		err    string
	}{
		{
			name:   "empty single-line array",
			before: "[workspace]\nmembers = []\nexclude = [\".worktrees\"]\nresolver = \"3\"\n",
			id:     "pub",
			after:  "[workspace]\nmembers = [\"pub\"]\nexclude = [\".worktrees\"]\nresolver = \"3\"\n",
		},
		{
			name:   "single-line sorted insert",
			before: "[workspace]\nmembers = [\"aaa\", \"zzz\"]\n",
			id:     "mmm",
			after:  "[workspace]\nmembers = [\"aaa\", \"mmm\", \"zzz\"]\n",
		},
		{
			name:   "single-line trailing comment preserved",
			before: "[workspace]\nmembers = [\"aaa\"] # crates live here\nresolver = \"3\"\n",
			id:     "bbb",
			after:  "[workspace]\nmembers = [\"aaa\", \"bbb\"] # crates live here\nresolver = \"3\"\n",
		},
		{
			name:   "multi-line insert in the middle",
			before: "[workspace]\nmembers = [\n  \"aaa\",\n  \"zzz\",\n]\nexclude = [\".worktrees\"]\n",
			id:     "mmm",
			after:  "[workspace]\nmembers = [\n  \"aaa\",\n  \"mmm\",\n  \"zzz\",\n]\nexclude = [\".worktrees\"]\n",
		},
		{
			name:   "multi-line comment on entries preserved",
			before: "# workspace root\n[workspace]\nmembers = [\n  \"aaa\", # first\n]\n",
			id:     "bbb",
			after:  "# workspace root\n[workspace]\nmembers = [\n  \"aaa\", # first\n  \"bbb\",\n]\n",
		},
		{
			name:   "multi-line empty array",
			before: "[workspace]\nmembers = [\n]\n",
			id:     "pub",
			after:  "[workspace]\nmembers = [\n  \"pub\",\n]\n",
		},
		{
			name:   "comments elsewhere preserved byte for byte",
			before: "# header comment\n[workspace]\nmembers = []\n\n[other]\nkey = 1 # inline\n",
			id:     "pub",
			after:  "# header comment\n[workspace]\nmembers = [\"pub\"]\n\n[other]\nkey = 1 # inline\n",
		},
		{
			name:   "already a member",
			before: "[workspace]\nmembers = [\"pub\"]\n",
			id:     "pub",
			err:    "already lists \"pub\" as a member",
		},
		{
			name:   "no workspace table",
			before: "[package]\nname = \"root\"\n",
			id:     "pub",
			err:    "has no [workspace] table",
		},
		{
			name:   "members key absent",
			before: "[workspace]\nresolver = \"3\"\n",
			id:     "pub",
			err:    "no members array in an editable form",
		},
		{
			name:   "two entries on one line in a multi-line array",
			before: "[workspace]\nmembers = [\n  \"aaa\", \"bbb\",\n]\n",
			id:     "pub",
			err:    "not one plain quoted string per line",
		},
		{
			name:   "non-string members rejected at parse",
			before: "[workspace]\nmembers = [\n  { name = \"a\" },\n]\n",
			id:     "pub",
			err:    "is not valid",
		},
		{
			name:   "unsupported member charset",
			before: "[workspace]\nmembers = []\n",
			id:     "su/b",
			err:    "unsupported workspace member name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "Cargo.toml")
			if err := os.WriteFile(p, []byte(tt.before), 0o644); err != nil {
				t.Fatal(err)
			}
			err := addWorkspaceMember(root, tt.id)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want containing %q", err, tt.err)
				}
				if after, rerr := os.ReadFile(p); rerr != nil || string(after) != tt.before {
					t.Fatalf("failed edit must not write: %q (%v)", after, rerr)
				}
				return
			}
			if err != nil {
				t.Fatalf("addWorkspaceMember: %v", err)
			}
			after, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tt.after {
				t.Errorf("edited manifest:\ngot:\n%s\nwant:\n%s", after, tt.after)
			}
		})
	}
}
