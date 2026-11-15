package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func promptFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".za", "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const promptBody = "\n\n## Problem & Goal\n\nCarry the work items.\n"

func TestPromptWorkspaceOnly(t *testing.T) {
	root := promptFixture(t, map[string]string{
		".za/issues/I-000001.md": "2026-11-15 Workspace note +workspace @todo id:I-000001" + promptBody,
	})
	prompt, err := Prompt(root, "I-000001")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	for _, want := range []string{
		"# Task: I-000001 — Workspace note\n",
		"- Priority: -\n",
		"- Status: @todo\n",
		"- Created: 2026-11-15\n",
		"- Projects: workspace\n",
		"- `AGENTS.md`\n",
		"- `.agents/context/AGENTS.md`\n",
		"- `.agents/context/policy.md`\n",
		"- Workspace-level items (policy, private context, scaffolding, root integration) change in the private meta-repository itself, never in a project repository (policy P-15)\n",
		"## Issue specification\n\n## Problem & Goal\n\nCarry the work items.\n",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	// A workspace-only issue routes to no project context.
	if strings.Contains(prompt, ".agents/context/go-za/") {
		t.Errorf("prompt routes to a project context although only +workspace is named:\n%s", prompt)
	}
	if !strings.HasSuffix(prompt, "Carry the work items.\n") {
		t.Errorf("the issue body is not verbatim at the end:\n%s", prompt)
	}
}

func TestPromptWithProjectSpecAndADR(t *testing.T) {
	root := promptFixture(t, map[string]string{
		".za/issues/I-000002.md":              "(A) 2026-11-15 Implement wiki CLI +go-za +go-minolas @doing id:I-000002 spec:go-minolas-architecture adr:D-0001" + promptBody,
		".za/wiki/go-minolas-architecture.md": "# go-minolas: Technical Architecture\n",
		"go-za/go.mod":                        "module github.com/tnotstar/go-za\n\ngo 1.24\n",
	})
	prompt, err := Prompt(root, "I-000002")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	for _, want := range []string{
		"# Task: I-000002 — Implement wiki CLI\n",
		"- Priority: A\n",
		"- Status: @doing\n",
		"- Projects: go-za, go-minolas\n",
		"- `.agents/context/go-za/AGENTS.md`\n",
		"- `.agents/context/go-za/sot.md`\n",
		"- `.agents/context/go-za/tools.md`\n",
		"- `.agents/context/go-za/adr.md`\n",
		"- `.agents/context/go-za/status.md`\n",
		"- `.agents/context/go-minolas/AGENTS.md`\n",
		"- `.za/wiki/go-minolas-architecture.md` (specification wiki page)\n",
		"- Decision D-0001 — record index: `.agents/context/go-za/adr.md`\n",
		"- Decision D-0001 — record index: `.agents/context/go-minolas/adr.md`\n",
		"- go-za: implement in the task worktree `.worktrees/go-za/I-000002/` created per policy P-15; validate standalone with `GOWORK=off` (root-module Go project)\n",
		"- go-minolas: implement in the task worktree `.worktrees/go-minolas/I-000002/` created per policy P-15; validate per the project's `tools.md`\n",
		"## Issue specification\n\n## Problem & Goal\n\nCarry the work items.\n",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestPromptProjectWithoutGoMod(t *testing.T) {
	root := promptFixture(t, map[string]string{
		".za/issues/I-000003.md": "2026-11-15 Rust work +some-crate @todo id:I-000003" + promptBody,
		"some-crate/Cargo.toml":  "[workspace]\n",
	})
	prompt, err := Prompt(root, "I-000003")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	want := "- some-crate: implement in the task worktree `.worktrees/some-crate/I-000003/` created per policy P-15; validate per the project's `tools.md`\n"
	if !strings.Contains(prompt, want) {
		t.Errorf("prompt lacks %q:\n%s", want, prompt)
	}
}

func TestPromptSpecForms(t *testing.T) {
	record := func(spec string) string {
		return "(B) 2026-11-15 T +go-za @todo id:I-000004 spec:" + spec + promptBody
	}
	root := promptFixture(t, map[string]string{
		".za/issues/I-000004.md":   record("architecture"),
		".za/wiki/architecture.md": "# Architecture\n",
		".za/issues/I-000005.md":   "(B) 2026-11-15 T +go-za @todo id:I-000005 spec:.za/wiki/deep/page.md" + promptBody,
		".za/wiki/deep/page.md":    "# Deep\n",
	})
	if _, err := Prompt(root, "I-000004"); err != nil {
		t.Errorf("bare slug spec: %v", err)
	}
	if _, err := Prompt(root, "I-000005"); err != nil {
		t.Errorf("explicit wiki path spec: %v", err)
	}
}

func TestPromptFailsClosed(t *testing.T) {
	t.Run("missing spec page", func(t *testing.T) {
		root := promptFixture(t, map[string]string{
			".za/issues/I-000004.md": "(B) 2026-11-15 T +go-za @todo id:I-000004 spec:missing-page" + promptBody,
		})
		_, err := Prompt(root, "I-000004")
		if err == nil || !strings.Contains(err.Error(), "spec target .za/wiki/missing-page.md does not exist") {
			t.Errorf("Prompt error = %v, want a missing spec target", err)
		}
	})
	t.Run("missing record", func(t *testing.T) {
		root := promptFixture(t, nil)
		_, err := Prompt(root, "I-000099")
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("Prompt error = %v, want does not exist", err)
		}
	})
	t.Run("malformed record", func(t *testing.T) {
		root := promptFixture(t, map[string]string{
			".za/issues/I-000004.md": "2026-11-15 T @blocked id:I-000004\n\n",
		})
		_, err := Prompt(root, "I-000004")
		if err == nil || !strings.Contains(err.Error(), "unknown status tag") {
			t.Errorf("Prompt error = %v, want a grammar error", err)
		}
	})
}
