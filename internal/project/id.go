package project

import (
	"fmt"
	"strings"
)

// reservedIDs are names a project directory must never take at the workspace
// root because za or a native workspace tool owns them.
var reservedIDs = map[string]bool{
	"AGENTS.md":      true,
	"CLAUDE.md":      true,
	"go.work":        true,
	"go.work.sum":    true,
	"pyproject.toml": true,
	"uv.lock":        true,
	"za.toml":        true,
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
	case reservedIDs[id]:
		return fmt.Errorf("%q is reserved for a workspace file", id)
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return fmt.Errorf("%q contains %q; only ASCII letters, digits, \".\", \"_\" and \"-\" are allowed", id, r)
		}
	}
	return nil
}
