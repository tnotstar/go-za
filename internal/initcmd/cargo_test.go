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

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want version
		err  string
	}{
		{in: "1.99.1", want: version{1, 99, 1}},
		{in: "1.85.0", want: version{1, 85, 0}},
		{in: "1.86.0-nightly", want: version{1, 86, 0}},
		{in: "1.2", err: "unrecognized version"},
		{in: "x.y.z", err: "unrecognized version"},
		{in: "", err: "unrecognized version"},
	}
	for _, tt := range tests {
		got, err := parseVersion(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("parseVersion(%q) = %+v, %v; want error containing %q", tt.in, got, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("parseVersion(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
}

func TestProbeVersion(t *testing.T) {
	if got, err := probeVersion("cargo 1.99.1 (5f94df478 2026-08-27)\n"); err != nil || got != (version{1, 99, 1}) {
		t.Errorf("probeVersion(cargo) = %+v, %v", got, err)
	}
	if got, err := probeVersion("rustc 1.85.0 (4d91de4e4 2025-02-17)\n"); err != nil || got != (version{1, 85, 0}) {
		t.Errorf("probeVersion(rustc) = %+v, %v", got, err)
	}
	if _, err := probeVersion("some other tool 1.0.0"); err == nil || !strings.Contains(err.Error(), "unrecognized version output") {
		t.Errorf("probeVersion(other) = %v", err)
	}
}

func TestVersionFloor(t *testing.T) {
	if (version{1, 85, 0}).olderThan(minRust()) {
		t.Error("1.85.0 must satisfy the supported floor")
	}
	if !(version{1, 84, 9}).olderThan(minRust()) {
		t.Error("1.84.9 must fail the supported floor")
	}
	if (version{2, 0, 0}).olderThan(minRust()) {
		t.Error("2.0.0 must satisfy the supported floor")
	}
	if (version{1, 85, 1}).olderThan(minRust()) {
		t.Error("1.85.1 must satisfy the supported floor")
	}
}

func TestCargoEnv(t *testing.T) {
	without := cargoEnv("")
	if len(without) != 1 || without[0] != "RUSTUP_AUTO_INSTALL=0" {
		t.Errorf("cargoEnv(\"\") = %v, want only the auto-install guard", without)
	}
	with := cargoEnv("stable-x86_64-unknown-linux-gnu")
	want := []string{"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TOOLCHAIN=stable-x86_64-unknown-linux-gnu"}
	if len(with) != 2 || with[0] != want[0] || with[1] != want[1] {
		t.Errorf("cargoEnv(pin) = %v, want %v", with, want)
	}
}

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
