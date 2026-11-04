package project

import (
	"fmt"
	"strings"
)

// reservedIDs are names a project directory must never take. Workspace-root
// files are owned by za or native tools; the context names are reserved
// direct children of .agents/context/ under the workspace schema. Existing
// user-authored COMMON topics also occupy that namespace and are checked
// separately against the live context directory.
//
// Comparison is case-insensitive: on case-insensitive filesystems a
// case-equivalent name would collide with the reserved entry.
var reservedIDs = []string{
	// Workspace-root files.
	"AGENTS.md",
	"CLAUDE.md",
	"go.work",
	"go.work.sum",
	"pyproject.toml",
	"uv.lock",
	"za.toml",
	"Cargo.toml",
	"Cargo.lock",
	// Reserved direct children of .agents/context/.
	"AGENTS.md",
	"policy.md",
	"tooling.md",
	"templates",
	"issues",
}

// DeriveID returns the project ID for a repository URL: its final path
// component without trailing slashes and without one trailing ".git".
//
// The result is never sanitized into a different name. An ID that cannot
// safely become a top-level directory is an error.
func DeriveID(repoURL string) (string, error) {
	trimmed := strings.TrimRight(repoURL, "/")
	name := trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		name = trimmed[i+1:]
	} else if i := strings.LastIndex(trimmed, ":"); i >= 0 {
		// scp-like syntax without a path separator: host:repo.git
		name = trimmed[i+1:]
	}
	name = strings.TrimSuffix(name, ".git")

	if err := validateID(name); err != nil {
		return "", fmt.Errorf("cannot derive a project ID from %q: %w", repoURL, err)
	}
	return name, nil
}

// validateID accepts only a single, portable path component made of ASCII
// letters, digits, ".", "_" and "-", that is neither hidden nor option-like.
func validateID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("the repository name is empty")
	case id == "." || id == "..":
		return fmt.Errorf("%q is not a valid directory name", id)
	case strings.ContainsAny(id, `/\`):
		return fmt.Errorf("%q contains a path separator", id)
	case id[0] == '.':
		return fmt.Errorf("%q would be a hidden top-level directory", id)
	case id[0] == '-':
		return fmt.Errorf("%q starts with \"-\"", id)
	}
	for _, name := range reservedIDs {
		if strings.EqualFold(id, name) {
			return fmt.Errorf("%q is reserved: it collides with workspace file or context name %q", id, name)
		}
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return fmt.Errorf("%q contains %q; only ASCII letters, digits, \".\", \"_\" and \"-\" are allowed", id, r)
		}
	}
	return nil
}
