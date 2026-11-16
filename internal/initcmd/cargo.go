// Init-specific Cargo validation: "za init" proves the scaffold it wrote is
// the expected empty virtual workspace. The shared Rust toolchain discipline
// (version floor, hermetic environment, offline metadata) lives in
// internal/cargo.
package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tnotstar/go-za/internal/cargo"
)

// cargoValidate re-checks the written manifest and proves, with real Cargo,
// that the initialized workspace is the expected empty virtual workspace:
// the metadata operation must resolve the workspace root to the target,
// report zero members and zero packages, and stay offline.
func (in *initializer) cargoValidate(pin string, manifest []byte) error {
	written, err := os.ReadFile(filepath.Join(in.root, "Cargo.toml"))
	if err != nil {
		return fmt.Errorf("read generated Cargo.toml: %w", err)
	}
	if string(written) != string(manifest) {
		return errors.New("generated Cargo.toml is not the expected virtual workspace manifest")
	}

	m, err := cargo.MetadataFor(in.ctx, in.runner, in.root, pin)
	if err != nil {
		return err
	}
	switch {
	case len(m.WorkspaceMembers) != 0:
		return fmt.Errorf("cargo metadata reported %d workspace members, want 0 for the empty bootstrap workspace", len(m.WorkspaceMembers))
	case len(m.Packages) != 0:
		return fmt.Errorf("cargo metadata reported %d packages, want 0: the empty scaffold must not create packages", len(m.Packages))
	}
	return nil
}
