package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tnotstar/go-za/internal/tool"
	"github.com/tnotstar/go-za/internal/tool/tooltest"
)

const validationManifest = "[workspace]\nmembers = []\nexclude = [\".worktrees\"]\nresolver = \"3\"\n"

// TestCargoValidate covers the metadata validation contract with a fake
// runner: nonzero exit, truncated JSON, missing fields, wrong root, and any
// implicit package creation must fail validation.
func TestCargoValidate(t *testing.T) {
	// newValidationInit writes the expected manifest and wires respond,
	// which receives the workspace root so responses can reference it.
	newValidationInit := func(t *testing.T, respond func(root string) func(tool.Command) ([]byte, error)) *initializer {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(validationManifest), 0o644); err != nil {
			t.Fatal(err)
		}
		return &initializer{ctx: t.Context(), runner: &tooltest.Fake{Respond: respond(root)}, root: root}
	}
	valid := func(root string) func(tool.Command) ([]byte, error) {
		return func(c tool.Command) ([]byte, error) {
			manifestPath := ""
			for i, a := range c.Args {
				if a == "--manifest-path" {
					manifestPath = c.Args[i+1]
				}
			}
			return []byte(fmt.Sprintf(`{"workspace_root":%q,"workspace_members":[],"packages":[]}`, filepath.Dir(manifestPath))), nil
		}
	}

	t.Run("validates the empty bootstrap workspace", func(t *testing.T) {
		in := newValidationInit(t, valid)
		if err := in.cargoValidate("", []byte(validationManifest)); err != nil {
			t.Errorf("cargoValidate: %v", err)
		}
	})

	t.Run("cargo nonzero exit", func(t *testing.T) {
		in := newValidationInit(t, func(string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) { return nil, errors.New("error: failed to parse manifest") }
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "cargo metadata validation failed") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("truncated JSON", func(t *testing.T) {
		in := newValidationInit(t, func(string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) {
				return []byte(`{"workspace_root":"/x","workspace_memb`), nil
			}
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		in := newValidationInit(t, func(root string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) {
				return []byte(fmt.Sprintf(`{"workspace_root":%q}`, root)), nil
			}
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "lacks workspace_members") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("unexpected workspace root", func(t *testing.T) {
		in := newValidationInit(t, func(string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) {
				return []byte(`{"workspace_root":"/elsewhere","workspace_members":[],"packages":[]}`), nil
			}
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "reported workspace root") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("implicit member creation", func(t *testing.T) {
		in := newValidationInit(t, func(root string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) {
				return []byte(fmt.Sprintf(`{"workspace_root":%q,"workspace_members":["x 0.1.0 (path+file:///x)"],"packages":[]}`, root)), nil
			}
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "want 0") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("implicit package creation", func(t *testing.T) {
		in := newValidationInit(t, func(root string) func(tool.Command) ([]byte, error) {
			return func(tool.Command) ([]byte, error) {
				return []byte(fmt.Sprintf(`{"workspace_root":%q,"workspace_members":[],"packages":[{"name":"x"}]}`, root)), nil
			}
		})
		if err := in.cargoValidate("", []byte(validationManifest)); err == nil || !strings.Contains(err.Error(), "must not create packages") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("modified manifest", func(t *testing.T) {
		in := newValidationInit(t, valid)
		if err := in.cargoValidate("", []byte(validationManifest+"\n# edited\n")); err == nil || !strings.Contains(err.Error(), "not the expected virtual workspace manifest") {
			t.Errorf("err = %v", err)
		}
	})
}
