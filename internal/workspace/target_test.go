package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectDoesNotMutate(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "a", "b")
	tg, err := Inspect(target)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if tg.Existed() {
		t.Error("Existed() = true for a missing target")
	}
	if _, err := os.Stat(filepath.Join(parent, "a")); !os.IsNotExist(err) {
		t.Error("Inspect created directories")
	}
}

func TestInspectRejectsEmptyPath(t *testing.T) {
	if _, err := Inspect(""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestCreateAndCleanupNewTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "a", "b")
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(target, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tg.Cleanup([]string{".git", "never-created"}); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "a")); !os.IsNotExist(err) {
		t.Error("created ancestors were not removed")
	}
}

func TestCleanupKeepsUnknownEntriesAndParents(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "ws")
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(target, "user.txt")
	if err := os.WriteFile(user, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err = tg.Cleanup(nil)
	if err == nil || !strings.Contains(err.Error(), "left") {
		t.Fatalf("Cleanup error = %v, want report of entries left in place", err)
	}
	if _, err := os.Stat(user); err != nil {
		t.Errorf("unknown entry removed: %v", err)
	}
}

func TestCleanupExistingTargetKeepsDirectory(t *testing.T) {
	target := t.TempDir()
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tg.Cleanup([]string{"AGENTS.md"}); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("pre-existing target removed: %v", err)
	}
}

func TestCleanupRejectsNonLocalNames(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "ws")
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err != nil {
		t.Fatal(err)
	}

	err = tg.Cleanup([]string{"../outside", "..", ".", "", filepath.Join("a", "b")})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("Cleanup error = %v, want refusal", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside the target was removed: %v", err)
	}
}

func TestCleanupSkipsReplacedTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "ws")
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err != nil {
		t.Fatal(err)
	}
	// Replace the directory with a different one holding user data.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := tg.Cleanup([]string{"AGENTS.md"}); err == nil {
		t.Fatal("expected Cleanup to refuse a replaced target")
	}
	if _, err := os.Stat(filepath.Join(target, "AGENTS.md")); err != nil {
		t.Errorf("replaced target content removed: %v", err)
	}
}

func TestCleanupBeforeCreateIsNoop(t *testing.T) {
	tg, err := Inspect(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Cleanup([]string{"x"}); err != nil {
		t.Errorf("Cleanup before Create: %v", err)
	}
}

func TestCreateRejectsTargetFilledAfterInspect(t *testing.T) {
	target := t.TempDir()
	tg, err := Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "late.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tg.Create(); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("Create error = %v, want not-empty failure", err)
	}
}
