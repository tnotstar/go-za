package project

import (
	"strings"
	"testing"
)

func TestDeriveID(t *testing.T) {
	valid := map[string]string{
		"https://github.com/org/go-za.git":  "go-za",
		"https://github.com/org/go-za":      "go-za",
		"https://github.com/org/go-za/":     "go-za",
		"https://github.com/org/go-za.git/": "go-za",
		"git@github.com:org/go-za.git":      "go-za",
		"git@host:go-za.git":                "go-za",
		"ssh://git@host:2222/org/go-za.git": "go-za",
		"file:///srv/git/go_za.v2.git":      "go_za.v2",
		"/srv/git/foo":                      "foo",
		"../foo.git":                        "foo",
		"https://host/org/repo.git.git":     "repo.git",
		// Suffix-less names that merely resemble reserved files stay valid.
		"https://github.com/org/principles": "principles",
		"https://github.com/org/policies":   "policies",
	}
	for in, want := range valid {
		got, err := DeriveID(in)
		if err != nil || got != want {
			t.Errorf("DeriveID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	invalid := map[string]string{
		"":                               "empty",
		"/":                              "empty",
		"https://github.com/org/.git":    "empty",
		"https://github.com/org/..":      "not a valid directory name",
		"https://github.com/org/.":       "not a valid directory name",
		`C:\repos\foo`:                   "path separator",
		"https://github.com/org/.hid":    "hidden",
		"https://github.com/org/-rf":     "starts with",
		"https://github.com/org/a b":     "only ASCII",
		"https://github.com/org/a?x=1":   "only ASCII",
		"https://github.com/org/ñu":      "only ASCII",
		"https://github.com/org/za.toml": "reserved",
		"https://github.com/org/go.work": "reserved",
		// Reserved context names under the schema-4 flat layout.
		"https://github.com/org/issues":     "reserved",
		"https://github.com/org/templates":  "reserved",
		"https://github.com/org/AGENTS.md":  "reserved",
		"https://github.com/org/policy.md":  "reserved",
		"https://github.com/org/tooling.md": "reserved",
		"https://github.com/org/CLAUDE.md":  "reserved",
		"https://github.com/org/sot.md":     "not reserved",
		"https://github.com/org/adr.md":     "not reserved",
		// Rust names, reserved regardless of the workspace's capabilities.
		"https://github.com/org/Cargo.toml": "reserved",
		"https://github.com/org/Cargo.lock": "reserved",
		"https://github.com/org/cargo.toml": "reserved",
		// Case-equivalent names collide on case-insensitive filesystems.
		"https://github.com/org/Policy.md":  "reserved",
		"https://github.com/org/ISSUES":     "reserved",
		"https://github.com/org/Templates":  "reserved",
		"https://github.com/org/ZA.TOML":    "reserved",
		"https://github.com/org/agents.md":  "reserved",
		"https://github.com/org/Tooling.md": "reserved",
	}
	for in, want := range invalid {
		got, err := DeriveID(in)
		if want == "not reserved" {
			if err != nil {
				t.Errorf("DeriveID(%q) = %q, %v; want a valid ID", in, got, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("DeriveID(%q) = %q, %v; want error containing %q", in, got, err, want)
		}
	}
}
