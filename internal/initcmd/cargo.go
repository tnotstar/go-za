// Rust capability probes and native validation for "za init".
//
// za never installs, updates or downloads Rust toolchains. All invocations
// set RUSTUP_AUTO_INSTALL=0, which rustup 1.28+ honors to suppress implicit
// toolchain installation, and pin RUSTUP_TOOLCHAIN to the toolchain resolved
// at preflight, so that ancestor rust-toolchain.toml files or ambient
// overrides cannot redirect (and thereby download) a toolchain during
// validation. When rustup is absent, cargo is assumed to be a native
// installation, which ignores rust-toolchain files entirely.
package initcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tnotstar/go-za/internal/tool"
)

// MinRustVersion is the supported floor for za's Rust capability: the
// explicit minimum for resolver 3 and Rust 2024 support. It constrains only
// running za's Rust capability, never a project's own rust-version or
// edition.
const MinRustVersion = "1.85.0"

// cargoPin returns the toolchain za pins its Cargo invocations to, and the
// environment entries carrying that pin. An empty pin means no rustup-managed
// selection applies.
func cargoEnv(pin string) []string {
	env := []string{"RUSTUP_AUTO_INSTALL=0"}
	if pin != "" {
		env = append(env, "RUSTUP_TOOLCHAIN="+pin)
	}
	return env
}

// rustPreflight verifies the installed Rust toolchain before any mutation:
// executables present, effective versions at or above MinRustVersion, and a
// locally installed toolchain selection. It returns the toolchain name the
// run pins, empty when rustup does not manage the installation.
func rustPreflight(ctx context.Context, r tool.Runner) (string, error) {
	for _, name := range []string{"cargo", "rustc"} {
		if _, err := r.LookPath(name); err != nil {
			return "", fmt.Errorf("Rust workspace requested but executable %q was not found in PATH", name)
		}
	}

	pin := ""
	if _, err := r.LookPath("rustup"); err == nil {
		out, err := r.Output(ctx, tool.Command{
			Name: "rustup", Args: []string{"show", "active-toolchain"},
			Env: cargoEnv(""),
		})
		if err != nil {
			return "", fmt.Errorf("determine the active Rust toolchain: %w\nresolve the ambient override (rust-toolchain.toml or RUSTUP_TOOLCHAIN) yourself; za never installs or downloads toolchains", err)
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) == 0 {
			return "", errors.New("rustup reported no active toolchain")
		}
		pin = fields[0]
	}

	for _, probe := range []struct{ name string }{{"cargo"}, {"rustc"}} {
		out, err := r.Output(ctx, tool.Command{
			Name: probe.name, Args: []string{"--version"}, Env: cargoEnv(pin),
		})
		if err != nil {
			return "", fmt.Errorf("determine the installed %s version: %w", probe.name, err)
		}
		version, err := probeVersion(string(out))
		if err != nil {
			return "", fmt.Errorf("%s: %w", probe.name, err)
		}
		if version.olderThan(minRust()) {
			return "", fmt.Errorf("%s %s is older than the supported floor %s for za's Rust capability; upgrade Rust yourself, za never installs or downloads toolchains", probe.name, version, MinRustVersion)
		}
	}
	return pin, nil
}

// probeVersion parses "cargo 1.99.1 (hash date)" or "rustc 1.85.0 (...)". A
// pre-release suffix is ignored for the floor comparison.
func probeVersion(out string) (version, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 || fields[0] != "cargo" && fields[0] != "rustc" {
		return version{}, fmt.Errorf("unrecognized version output %q", strings.TrimSpace(out))
	}
	return parseVersion(fields[1])
}

type version struct{ major, minor, patch int }

func parseVersion(s string) (version, error) {
	s, _, _ = strings.Cut(s, "-")
	var v version
	if n, err := fmt.Sscanf(s, "%d.%d.%d", &v.major, &v.minor, &v.patch); err != nil || n != 3 {
		return version{}, fmt.Errorf("unrecognized version %q", s)
	}
	return v, nil
}

func minRust() version {
	v, err := parseVersion(MinRustVersion)
	if err != nil {
		panic("MinRustVersion must parse: " + err.Error())
	}
	return v
}

func (v version) olderThan(o version) bool {
	switch {
	case v.major != o.major:
		return v.major < o.major
	case v.minor != o.minor:
		return v.minor < o.minor
	default:
		return v.patch < o.patch
	}
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// cargoMetadata is the subset of "cargo metadata --format-version 1" output
// za validates. Unknown fields are ignored for forward compatibility.
type cargoMetadata struct {
	WorkspaceRoot    string            `json:"workspace_root"`
	WorkspaceMembers []string          `json:"workspace_members"`
	Packages         []json.RawMessage `json:"packages"`
}

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

	manifestPath := filepath.Join(in.root, "Cargo.toml")
	out, err := in.runner.Output(in.ctx, tool.Command{
		Name: "cargo",
		Args: []string{
			"metadata", "--format-version", "1", "--no-deps", "--offline",
			"--manifest-path", manifestPath,
		},
		Dir: in.root,
		Env: cargoEnv(pin),
	})
	if err != nil {
		return fmt.Errorf("cargo metadata validation failed: %w", err)
	}

	var m cargoMetadata
	if err := json.Unmarshal(out, &m); err != nil {
		return fmt.Errorf("cargo metadata returned invalid JSON: %w", err)
	}
	switch {
	case m.WorkspaceRoot == "":
		return errors.New("cargo metadata reported no workspace root")
	case !sameDirPath(m.WorkspaceRoot, in.root):
		return fmt.Errorf("cargo metadata reported workspace root %q, want the initialized workspace %q", m.WorkspaceRoot, in.root)
	case m.WorkspaceMembers == nil:
		return errors.New("cargo metadata response lacks workspace_members")
	case m.Packages == nil:
		return errors.New("cargo metadata response lacks packages")
	case len(m.WorkspaceMembers) != 0:
		return fmt.Errorf("cargo metadata reported %d workspace members, want 0 for the empty bootstrap workspace", len(m.WorkspaceMembers))
	case len(m.Packages) != 0:
		return fmt.Errorf("cargo metadata reported %d packages, want 0: the empty scaffold must not create packages", len(m.Packages))
	}
	return nil
}

// sameDirPath reports whether a and b name the same directory, tolerating
// symlink resolution differences between za and Cargo.
func sameDirPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
