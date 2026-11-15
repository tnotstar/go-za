package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// workspaceFixture builds a minimal valid za workspace with an issue
// directory and returns its root.
func workspaceFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
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

func TestRoot(t *testing.T) {
	t.Run("finds the workspace and requires .za/issues", func(t *testing.T) {
		root := workspaceFixture(t, nil)
		env := Env{Getwd: func() (string, error) { return filepath.Join(root, "deep", "nested"), nil }}
		// The nested directory does not exist; discovery starts from the name.
		if err := os.MkdirAll(filepath.Join(root, "deep", "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := env.Root()
		if err != nil || got != root {
			t.Errorf("Root = %q, %v; want %q", got, err, root)
		}
	})
	t.Run("outside a workspace", func(t *testing.T) {
		env := Env{Getwd: func() (string, error) { return t.TempDir(), nil }}
		if _, err := env.Root(); err == nil || !strings.Contains(err.Error(), "not inside a za workspace") {
			t.Errorf("Root error = %v, want not inside a za workspace", err)
		}
	})
	t.Run("missing .za/issues fails closed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 4\n\n[workspace]\ngo = true\npython = false\nrust = false\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		env := Env{Getwd: func() (string, error) { return root, nil }}
		_, err := env.Root()
		if err == nil || !strings.Contains(err.Error(), Dir+" is missing") {
			t.Errorf("Root error = %v, want containing %q", err, Dir+" is missing")
		}
	})
	t.Run("unsupported schema fails closed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "za.toml"), []byte("schema = 3\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
			t.Fatal(err)
		}
		env := Env{Getwd: func() (string, error) { return root, nil }}
		if _, err := env.Root(); err == nil || !strings.Contains(err.Error(), "schema") {
			t.Errorf("Root error = %v, want a schema rejection", err)
		}
	})
}

func TestCreateAndScan(t *testing.T) {
	root := workspaceFixture(t, nil)
	dir := IssuesDir(root)

	first, err := Create(dir, Issue{Status: Todo, Priority: 'B', Title: "First record", Projects: []string{"workspace"}}, DefaultBody, testNow)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.ID != "I-000001" || first.Created != "2026-11-15" {
		t.Errorf("unexpected record %+v", first)
	}
	second, err := Create(dir, Issue{Status: Doing, Title: "Second record", Created: "2026-11-01"}, DefaultBody, testNow)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if second.ID != "I-000002" || second.Created != "2026-11-01" || second.Priority != 0 {
		t.Errorf("the injected creation date must survive: %+v", second)
	}

	data, err := os.ReadFile(filepath.Join(dir, "I-000001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "(B) 2026-11-15 First record +workspace @todo id:I-000001\n" + DefaultBody
	if string(data) != want {
		t.Errorf("record content =\n%q\nwant\n%q", data, want)
	}

	issues, err := ScanSummaries(dir)
	if err != nil {
		t.Fatalf("ScanSummaries: %v", err)
	}
	if len(issues) != 2 || issues[0].ID != "I-000001" || issues[1].ID != "I-000002" {
		t.Errorf("scan after create = %+v", issues)
	}
}

func TestCreateRejectsDone(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, Issue{Status: Done, Title: "x"}, DefaultBody, testNow); err == nil {
		t.Error("Create accepted a @done record")
	}
}

func TestMoveTransitions(t *testing.T) {
	root := workspaceFixture(t, nil)
	dir := IssuesDir(root)
	created, err := Create(dir, Issue{Status: Todo, Priority: 'B', Title: "Follow the flow", Projects: []string{"go-za"}, Spec: ".za/wiki/index.md"}, DefaultBody, testNow)
	if err != nil {
		t.Fatal(err)
	}
	bodySuffix := DefaultBody

	doing, err := Move(dir, created.ID, Doing, "", testNow)
	if err != nil {
		t.Fatalf("move to doing: %v", err)
	}
	if doing.Status != Doing || doing.Completed != "" || doing.Commit != "" || doing.Spec != ".za/wiki/index.md" {
		t.Errorf("unexpected doing record %+v", doing)
	}

	doneTime := time.Date(2026, 11, 20, 23, 0, 0, 0, time.UTC)
	done, err := Move(dir, created.ID, Done, "abc123def", doneTime)
	if err != nil {
		t.Fatalf("move to done: %v", err)
	}
	if done.Status != Done || done.Completed != "2026-11-20" || done.Commit != "abc123def" {
		t.Errorf("unexpected done record %+v", done)
	}
	line1, err := LoadOne(dir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := Serialize(line1); got != "x 2026-11-20 2026-11-15 (B) Follow the flow +go-za @done id:I-000001 spec:.za/wiki/index.md commit:abc123def" {
		t.Errorf("done line = %s", got)
	}

	reopened, err := Move(dir, created.ID, Todo, "", doneTime)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Status != Todo || reopened.Completed != "" || reopened.Commit != "" {
		t.Errorf("reopening must strip the completion facts: %+v", reopened)
	}

	// The body (line 2 onwards) survives every transition byte for byte.
	data, err := os.ReadFile(filepath.Join(dir, created.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasSuffix(got, bodySuffix) {
		t.Errorf("body was modified by the moves:\n%q", got)
	}
	if _, after, _ := strings.Cut(got, "\n"); after != bodySuffix {
		t.Errorf("body (line 2 onwards) not preserved byte for byte:\n%q", after)
	}
}

func TestMoveErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Move(dir, "I-000001", Doing, "", testNow); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Move on a missing record = %v, want does not exist", err)
	}
	if _, err := Move(dir, "I-000001", "blocked", "", testNow); err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Errorf("Move to an unknown state = %v, want unknown status", err)
	}
	// A record whose line 2 is not blank is rejected before the rewrite.
	writeRecord(t, dir, "I-000001.md", "2026-11-15 Title @todo id:I-000001", "## Problem & Goal\n")
	if _, err := Move(dir, "I-000001", Doing, "", testNow); err == nil || !strings.Contains(err.Error(), "line 2 must be blank") {
		t.Errorf("Move on a malformed record = %v, want line 2 must be blank", err)
	}
}

func TestValidateProjects(t *testing.T) {
	root := workspaceFixture(t, map[string]string{
		".gitmodules": "[submodule \"go-za\"]\n\tpath = go-za\n\turl = https://example.invalid/go-za.git\n",
	})
	if err := os.MkdirAll(filepath.Join(root, "go-minolas"), 0o755); err != nil {
		t.Fatal(err)
	}
	valid := [][]string{
		{"workspace"},
		{"go-za"},
		{"go-minolas"}, // exists as a top-level directory
		{"workspace", "go-za"},
	}
	for _, projects := range valid {
		if err := validateProjects(root, projects); err != nil {
			t.Errorf("validateProjects(%v) = %v, want nil", projects, err)
		}
	}
	if err := validateProjects(root, []string{"go-za", "ghost"}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("validateProjects(ghost) = %v, want not registered", err)
	}
}

func TestTemplateBody(t *testing.T) {
	t.Run("falls back to the built-in skeleton", func(t *testing.T) {
		root := workspaceFixture(t, nil)
		body, err := TemplateBody(root)
		if err != nil || body != DefaultBody {
			t.Errorf("TemplateBody = %q, %v; want the built-in skeleton", body, err)
		}
	})
	t.Run("uses the workspace template without its grammar line", func(t *testing.T) {
		template := "[(A|B|C)] YYYY-MM-DD <Title> +<project-id> @todo id:I-NNNNNN\n\n## Problem & Goal\n\nTo be clarified.\n"
		root := workspaceFixture(t, map[string]string{TemplatePath: template})
		body, err := TemplateBody(root)
		if err != nil {
			t.Fatalf("TemplateBody: %v", err)
		}
		if body != "\n## Problem & Goal\n\nTo be clarified.\n" {
			t.Errorf("TemplateBody = %q", body)
		}
		// The composed record keeps line 2 blank.
		iss, err := Create(IssuesDir(root), Issue{Status: Todo, Title: "From template"}, body, testNow)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := LoadOne(IssuesDir(root), iss.ID); err != nil {
			t.Errorf("the template-backed record violates the line-2 invariant: %v", err)
		}
	})
}
