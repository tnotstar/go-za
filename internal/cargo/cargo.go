// Package cargo holds za's Rust toolchain discipline: the supported version
// floor, the hermetic environment for Cargo invocations, preflight checks
// and offline workspace-metadata validation. It is shared by "za init" and
// "za project add"; init-specific assertions stay with the init command.
//
// za never installs, updates or downloads Rust toolchains. All invocations
// set RUSTUP_AUTO_INSTALL=0, which rustup 1.28+ honors to suppress implicit
// toolchain installation, and pin RUSTUP_TOOLCHAIN to the toolchain resolved
// at preflight, so that ancestor rust-toolchain.toml files or ambient
// overrides cannot redirect (and thereby download) a toolchain during
// validation. When rustup is absent, cargo is assumed to be a native
// installation, which ignores rust-toolchain files entirely.
package cargo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tnotstar/go-za/internal/tool"
)

// MinVersion is the supported floor for za's Rust capability: the explicit
// minimum for resolver 3 and Rust 2024 support. It constrains only running
// za's Rust capability, never a project's own rust-version or edition.
const MinVersion = "1.85.0"

// EnvEntries returns the environment entries carrying the toolchain pin. An
// empty pin means no rustup-managed selection applies.
func EnvEntries(pin string) []string {
	env := []string{"RUSTUP_AUTO_INSTALL=0"}
	if pin != "" {
		env = append(env, "RUSTUP_TOOLCHAIN="+pin)
	}
	return env
}

// Preflight verifies the installed Rust toolchain before any mutation:
// executables present, effective versions at or above MinVersion, and a
// locally installed toolchain selection. It returns the toolchain name the
// run pins, empty when rustup does not manage the installation.
func Preflight(ctx context.Context, r tool.Runner) (string, error) {
	for _, name := range []string{"cargo", "rustc"} {
		if _, err := r.LookPath(name); err != nil {
			return "", fmt.Errorf("Rust capability requested but executable %q was not found in PATH", name)
		}
	}

	pin := ""
	if _, err := r.LookPath("rustup"); err == nil {
		out, err := r.Output(ctx, tool.Command{
			Name: "rustup", Args: []string{"show", "active-toolchain"},
			Env: EnvEntries(""),
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
			Name: probe.name, Args: []string{"--version"}, Env: EnvEntries(pin),
		})
		if err != nil {
			return "", fmt.Errorf("determine the installed %s version: %w", probe.name, err)
		}
		version, err := probeVersion(string(out))
		if err != nil {
			return "", fmt.Errorf("%s: %w", probe.name, err)
		}
		if version.olderThan(minVersion()) {
			return "", fmt.Errorf("%s %s is older than the supported floor %s for za's Rust capability; upgrade Rust yourself, za never installs or downloads toolchains", probe.name, version, MinVersion)
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

func minVersion() version {
	v, err := parseVersion(MinVersion)
	if err != nil {
		panic("MinVersion must parse: " + err.Error())
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

// Metadata is the subset of "cargo metadata --format-version 1" output za
// validates. Unknown fields are ignored for forward compatibility.
type Metadata struct {
	WorkspaceRoot    string            `json:"workspace_root"`
	WorkspaceMembers []string          `json:"workspace_members"`
	Packages         []json.RawMessage `json:"packages"`
}

// MetadataFor runs the offline no-deps "cargo metadata" against root's
// Cargo.toml with the toolchain pin applied, parses the response and checks
// that Cargo resolved the workspace root to root itself. Callers add their
// own assertions about members and packages on the returned Metadata.
func MetadataFor(ctx context.Context, r tool.Runner, root, pin string) (Metadata, error) {
	out, err := r.Output(ctx, tool.Command{
		Name: "cargo",
		Args: []string{
			"metadata", "--format-version", "1", "--no-deps", "--offline",
			"--manifest-path", filepath.Join(root, "Cargo.toml"),
		},
		Dir: root,
		Env: EnvEntries(pin),
	})
	if err != nil {
		return Metadata{}, fmt.Errorf("cargo metadata validation failed: %w", err)
	}

	var m Metadata
	if err := json.Unmarshal(out, &m); err != nil {
		return Metadata{}, fmt.Errorf("cargo metadata returned invalid JSON: %w", err)
	}
	switch {
	case m.WorkspaceRoot == "":
		return Metadata{}, errors.New("cargo metadata reported no workspace root")
	case !SameDirPath(m.WorkspaceRoot, root):
		return Metadata{}, fmt.Errorf("cargo metadata reported workspace root %q, want the workspace %q", m.WorkspaceRoot, root)
	case m.WorkspaceMembers == nil:
		return Metadata{}, errors.New("cargo metadata response lacks workspace_members")
	case m.Packages == nil:
		return Metadata{}, errors.New("cargo metadata response lacks packages")
	}
	return m, nil
}

// SameDirPath reports whether a and b name the same directory, tolerating
// symlink resolution differences between za and Cargo.
func SameDirPath(a, b string) bool {
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
