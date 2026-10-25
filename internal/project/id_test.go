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
	}
	for in, want := range invalid {
		got, err := DeriveID(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("DeriveID(%q) = %q, %v; want error containing %q", in, got, err, want)
		}
	}
}
