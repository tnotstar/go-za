package issue

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tnotstar/go-za/internal/wiki"
)

// Prompt renders the self-contained XP context prompt for one issue: the
// task header from the todo.txt line, the required context routing
// (workspace guides, per-project context, spec wiki pages and decision
// records), the execution boundary per policy P-15, and the verbatim
// issue specification body. The output is deterministic Markdown with
// workspace-relative paths only; no clock is read.
func Prompt(root, id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(IssuesDir(root), id+".md"))
	if err != nil {
		return "", fmt.Errorf("issue %s does not exist (%s)", id, RecordPath(id))
	}
	line1, rest, hasSeparator := strings.Cut(string(data), "\n")
	// The blank-line-2 shape is enforced like everywhere else, so a
	// malformed record is repaired by hand, never silently assembled into
	// a prompt.
	if hasSeparator && rest != "" && !strings.HasPrefix(rest, "\n") {
		return "", fmt.Errorf("%s: line 2 must be blank", RecordPath(id))
	}
	iss, err := Parse(line1)
	if err != nil {
		return "", fmt.Errorf("%s: %w", RecordPath(id), err)
	}
	if iss.ID != id {
		return "", fmt.Errorf("%s: the file name does not match the id tag %q", RecordPath(id), iss.ID)
	}

	// spec: targets must exist before the prompt is emitted, so an agent is
	// never routed to a page the workspace does not carry.
	var specPaths []string
	if iss.Spec != "" {
		p, err := specPath(root, iss.Spec)
		if err != nil {
			return "", err
		}
		specPaths = append(specPaths, p)
	}

	var b strings.Builder
	b.WriteString("# Task: " + iss.ID + " — " + iss.Title + "\n\n")
	if iss.Priority != 0 {
		fmt.Fprintf(&b, "- Priority: %c\n", iss.Priority)
	} else {
		b.WriteString("- Priority: -\n")
	}
	fmt.Fprintf(&b, "- Status: @%s\n", iss.Status)
	fmt.Fprintf(&b, "- Created: %s\n", iss.Created)
	if len(iss.Projects) > 0 {
		fmt.Fprintf(&b, "- Projects: %s\n", strings.Join(iss.Projects, ", "))
	}

	projects := nonWorkspaceProjects(iss.Projects)
	b.WriteString("\n## Required context\n\nRead these before any work:\n\n")
	for _, p := range []string{"AGENTS.md", ".agents/context/AGENTS.md", ".agents/context/policy.md"} {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	for _, p := range projects {
		for _, name := range []string{"AGENTS.md", "sot.md", "tools.md", "adr.md", "status.md"} {
			fmt.Fprintf(&b, "- `.agents/context/%s/%s`\n", p, name)
		}
	}
	for _, p := range specPaths {
		fmt.Fprintf(&b, "- `%s` (specification wiki page)\n", p)
	}
	if iss.ADR != "" {
		for _, p := range projects {
			fmt.Fprintf(&b, "- Decision %s — record index: `.agents/context/%s/adr.md`\n", iss.ADR, p)
		}
		if len(projects) == 0 {
			fmt.Fprintf(&b, "- Decision %s\n", iss.ADR)
		}
	}

	b.WriteString("\n## Execution boundary\n\n")
	for _, p := range projects {
		line := fmt.Sprintf("- %s: implement in the task worktree `.worktrees/%s/%s/` created per policy P-15", p, p, iss.ID)
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path.Join(p, "go.mod")))); err == nil {
			line += "; validate standalone with `GOWORK=off` (root-module Go project)"
		} else {
			line += "; validate per the project's `tools.md`"
		}
		b.WriteString(line + "\n")
	}
	if len(projects) == 0 || slices.Contains(iss.Projects, "workspace") {
		b.WriteString("- Workspace-level items (policy, private context, scaffolding, root integration) change in the private meta-repository itself, never in a project repository (policy P-15)\n")
	}

	// rest already starts with the blank line 2, so the verbatim body is
	// appended after the heading separator.
	b.WriteString("\n## Issue specification\n")
	b.WriteString(rest)
	return b.String(), nil
}

// nonWorkspaceProjects returns the named registered projects, dropping the
// workspace tag and preserving line order.
func nonWorkspaceProjects(projects []string) []string {
	var out []string
	for _, p := range projects {
		if p != "workspace" {
			out = append(out, p)
		}
	}
	return out
}

// specPath resolves a spec: tag value to a workspace-relative wiki page
// path and verifies the page exists. A value that already names a
// .za/wiki/ path is used as-is (gaining ".md" when missing); a bare value
// is treated as a slug.
func specPath(root, value string) (string, error) {
	var rel string
	if strings.HasPrefix(value, ".za/wiki/") {
		rel = value
		if !strings.HasSuffix(rel, ".md") {
			rel += ".md"
		}
	} else {
		slug, err := wiki.NormalizeSlug(value)
		if err != nil {
			return "", fmt.Errorf("invalid spec tag %q: %w", value, err)
		}
		rel = wiki.Path(slug)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		return "", fmt.Errorf("spec target %s does not exist in the workspace; create the page or fix the spec: tag", rel)
	}
	return rel, nil
}
