package cargo

import (
	"strings"
	"testing"
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
	if (version{1, 85, 0}).olderThan(minVersion()) {
		t.Error("1.85.0 must satisfy the supported floor")
	}
	if !(version{1, 84, 9}).olderThan(minVersion()) {
		t.Error("1.84.9 must fail the supported floor")
	}
	if (version{2, 0, 0}).olderThan(minVersion()) {
		t.Error("2.0.0 must satisfy the supported floor")
	}
	if (version{1, 85, 1}).olderThan(minVersion()) {
		t.Error("1.85.1 must satisfy the supported floor")
	}
}

func TestEnvEntries(t *testing.T) {
	without := EnvEntries("")
	if len(without) != 1 || without[0] != "RUSTUP_AUTO_INSTALL=0" {
		t.Errorf("EnvEntries(\"\") = %v, want only the auto-install guard", without)
	}
	with := EnvEntries("stable-x86_64-unknown-linux-gnu")
	want := []string{"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TOOLCHAIN=stable-x86_64-unknown-linux-gnu"}
	if len(with) != 2 || with[0] != want[0] || with[1] != want[1] {
		t.Errorf("EnvEntries(pin) = %v, want %v", with, want)
	}
}
