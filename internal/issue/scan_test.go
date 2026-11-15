package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRecord(t *testing.T, dir, name, line1 string, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(line1+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const bodyFixture = "\n## Problem & Goal\n\nTo be clarified.\n"

func TestScanSummaries(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, "I-000002.md", "(A) 2026-11-15 Second +go-za @doing id:I-000002", bodyFixture)
	writeRecord(t, dir, "I-000001.md", "2026-11-14 First +workspace @todo id:I-000001", bodyFixture)
	// A record that ends right after line 2 (blank line, then EOF) is valid.
	if err := os.WriteFile(filepath.Join(dir, "I-000003.md"), []byte("2026-11-14 Third @todo id:I-000003\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, dir, ".gitkeep", "", "")
	writeRecord(t, dir, "README.md", "not an issue", "")

	issues, err := ScanSummaries(dir)
	if err != nil {
		t.Fatalf("ScanSummaries: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("scanned %d issues, want 3: %+v", len(issues), issues)
	}
	if issues[0].ID != "I-000001" || issues[0].Title != "First" || issues[1].ID != "I-000002" || issues[2].ID != "I-000003" {
		t.Errorf("unexpected order or content: %+v", issues)
	}
}

func TestScanSummariesRejects(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		err   string
	}{
		{
			name:  "id does not match the file name",
			files: map[string]string{"I-000009.md": "2026-11-15 Title @todo id:I-000004\n\nbody\n"},
			err:   "does not match the id tag",
		},
		{
			name:  "line 2 is not blank",
			files: map[string]string{"I-000001.md": "2026-11-15 Title @todo id:I-000001\n## Problem & Goal\n"},
			err:   "line 2 must be blank",
		},
		{
			name:  "record line does not parse",
			files: map[string]string{"I-000001.md": "2026-11-15 Title @blocked id:I-000001\n\n"},
			err:   "unknown status tag",
		},
		{
			name:  "carriage return on the record line",
			files: map[string]string{"I-000001.md": "2026-11-15 Title @todo id:I-000001\r\n\n"},
			err:   "carriage return",
		},
		{
			name:  "record without the id tag",
			files: map[string]string{"I-000001.md": "2026-11-15 Title @todo\n\n"},
			err:   "missing id:I-NNNNNN tag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := ScanSummaries(dir)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ScanSummaries error = %v, want containing %q", err, tt.err)
			}
		})
	}
}

func TestNextID(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		id, err := NextID(dir)
		if err != nil || id != "I-000001" {
			t.Errorf("NextID = %q, %v; want I-000001", id, err)
		}
	})
	t.Run("ignores non-record entries", func(t *testing.T) {
		dir := t.TempDir()
		writeRecord(t, dir, ".gitkeep", "", "")
		writeRecord(t, dir, "notes.md", "text", "")
		id, err := NextID(dir)
		if err != nil || id != "I-000001" {
			t.Errorf("NextID = %q, %v; want I-000001", id, err)
		}
	})
	t.Run("increments beyond the maximum", func(t *testing.T) {
		dir := t.TempDir()
		writeRecord(t, dir, "I-000001.md", "2026-11-15 A @todo id:I-000001", "\n")
		writeRecord(t, dir, "I-000042.md", "2026-11-15 B @todo id:I-000042", "\n")
		writeRecord(t, dir, "I-000000.md", "2026-11-15 Zero @todo id:I-000000", "\n")
		id, err := NextID(dir)
		if err != nil || id != "I-000043" {
			t.Errorf("NextID = %q, %v; want I-000043", id, err)
		}
	})
	t.Run("exhausted ID space fails closed", func(t *testing.T) {
		dir := t.TempDir()
		writeRecord(t, dir, "I-999999.md", "2026-11-15 Last @todo id:I-999999", "\n")
		if id, err := NextID(dir); err == nil {
			t.Errorf("NextID = %q, want an error", id)
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		if _, err := NextID(filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Error("NextID on a missing directory succeeded")
		}
	})
}
