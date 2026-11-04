package version

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"runtime/debug"
)

// fixture builds a deterministic debug.BuildInfo; tests never depend on the
// build state of this test binary.
func fixture(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.27.2",
		Main:      debug.Module{Path: "github.com/tnotstar/go-za", Version: version},
		Settings:  settings,
	}
}

func rev(modified bool) []debug.BuildSetting {
	return []debug.BuildSetting{
		{Key: "vcs.revision", Value: "3996d08e0e873ecd3fcd38a755f7c9fc2a666d36"},
		{Key: "vcs.time", Value: "2026-11-04T11:26:48Z"},
		{Key: "vcs.modified", Value: map[bool]string{true: "true", false: "false"}[modified]},
	}
}

func inspectKind(t *testing.T, bi *debug.BuildInfo) Kind {
	t.Helper()
	return Inspect(bi).Kind
}

func TestInspectKinds(t *testing.T) {
	tests := []struct {
		name    string
		bi      *debug.BuildInfo
		want    Kind
		version string
	}{
		{name: "nil build info", bi: nil, want: Unknown, version: "unknown"},
		{name: "empty version", bi: fixture(""), want: Unknown, version: "unknown"},
		{name: "devel", bi: fixture("(devel)"), want: Development, version: "(devel)"},
		{name: "pseudo bare", bi: fixture("v0.0.0-20261104112648-3996d08e0e87"), want: PseudoVersion, version: "v0.0.0-20261104112648-3996d08e0e87"},
		{name: "pseudo after pre-release", bi: fixture("v1.2.3-pre.0.20261104112648-3996d08e0e87"), want: PseudoVersion},
		{name: "pseudo over patch", bi: fixture("v1.2.4-0.20261104112648-3996d08e0e87-abcdefabcdef"), want: PseudoVersion},
		{name: "pseudo without v prefix is not pseudo", bi: fixture("0.0.0-20261104112648-3996d08e0e87"), want: Unknown},
		{name: "dirty pseudo-version with modified flag is a development build", bi: fixture("v0.0.0-20261104145922-1671e67be376+dirty", rev(true)...), want: Development},
		{name: "dirty pseudo-version without modified flag stays pseudo", bi: fixture("v0.0.0-20261104145922-1671e67be376+dirty"), want: PseudoVersion},
		{name: "release", bi: fixture("v1.2.3"), want: Release},
		{name: "release with pre-release and build", bi: fixture("v1.2.3-rc.1+build.5"), want: Release},
		{name: "unrecognized version stays literal", bi: fixture("banana-20261104112648-3996d08e0e87"), want: Unknown, version: "banana-20261104112648-3996d08e0e87"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Inspect(tt.bi)
			if got.Kind != tt.want {
				t.Errorf("Kind = %d, want %d", got.Kind, tt.want)
			}
			if tt.version != "" && got.Version != tt.version {
				t.Errorf("Version = %q, want %q", got.Version, tt.version)
			}
		})
	}
}

func TestInspectVCSFields(t *testing.T) {
	t.Run("clean stamped devel build", func(t *testing.T) {
		got := Inspect(fixture("(devel)", rev(false)...))
		if got.Modified {
			t.Error("Modified = true, want false")
		}
		if got.Revision != "3996d08e0e873ecd3fcd38a755f7c9fc2a666d36" || got.CommitTime != "2026-11-04T11:26:48Z" {
			t.Errorf("revision/time = %q/%q", got.Revision, got.CommitTime)
		}
		if got.Module != "github.com/tnotstar/go-za" || got.GoVersion != "go1.27.2" {
			t.Errorf("module/goversion = %q/%q", got.Module, got.GoVersion)
		}
	})
	t.Run("modified source", func(t *testing.T) {
		got := Inspect(fixture("(devel)", rev(true)...))
		if !got.Modified {
			t.Error("Modified = false, want true")
		}
	})
	t.Run("no VCS settings", func(t *testing.T) {
		got := Inspect(fixture("(devel)"))
		if got.Revision != "" || got.Modified || got.CommitTime != "" {
			t.Errorf("VCS fields = %q/%v/%q, want empty", got.Revision, got.Modified, got.CommitTime)
		}
	})
	t.Run("module path absent", func(t *testing.T) {
		bi := fixture("(devel)")
		bi.Main.Path = ""
		if got := Inspect(bi); got.Module != "" {
			t.Errorf("Module = %q, want empty", got.Module)
		}
	})
}

func TestRender(t *testing.T) {
	tests := []struct {
		name   string
		info   Info
		want   []string
		absent []string
	}{
		{
			name: "release clean",
			info: Info{Kind: Release, Version: "v1.2.3", Module: "github.com/tnotstar/go-za", Revision: "abc", CommitTime: "2026-11-04T11:26:48Z", GoVersion: "go1.27.2"},
			want: []string{"za v1.2.3 (release)", "  module:    github.com/tnotstar/go-za", "  revision:  abc (clean)", "  committed: 2026-11-04T11:26:48Z", "  go:        go1.27.2"},
		},
		{
			name:   "development modified",
			info:   Info{Kind: Development, Version: "(devel)", Revision: "abc", Modified: true},
			want:   []string{"za (devel) (development build)", "  revision:  abc (modified)"},
			absent: []string{"committed:", "module:"},
		},
		{
			name: "pseudo-version",
			info: Info{Kind: PseudoVersion, Version: "v0.0.0-20261104112648-3996d08e0e87"},
			want: []string{"za v0.0.0-20261104112648-3996d08e0e87 (Go pseudo-version)"},
		},
		{
			name:   "no VCS metadata",
			info:   Info{Kind: Development, Version: "(devel)", GoVersion: "go1.27.2"},
			want:   []string{"za (devel) (development build)", "  revision:  unavailable (no VCS metadata stamped at build time)", "  go:        go1.27.2"},
			absent: []string{"committed:"},
		},
		{
			name:   "nil build info",
			info:   Inspect(nil),
			want:   []string{"za unknown (no build metadata)"},
			absent: []string{"revision:", "module:", "go:"},
		},
		{
			name: "unrecognized literal version",
			info: Info{Kind: Unknown, Version: "banana"},
			want: []string{"za banana (unrecognized build version)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tt.info.Render(&buf); err != nil {
				t.Fatalf("Render: %v", err)
			}
			if buf.Len() == 0 || !strings.HasSuffix(buf.String(), "\n") {
				t.Errorf("Render wrote %q, want newline-terminated non-empty output", buf.String())
			}
			for _, w := range tt.want {
				if !strings.Contains(buf.String(), w) {
					t.Errorf("Render output %q lacks %q", buf.String(), w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(buf.String(), a) {
					t.Errorf("Render output %q unexpectedly contains %q", buf.String(), a)
				}
			}
		})
	}
}

func TestRenderErrorPropagation(t *testing.T) {
	info := Info{Kind: Release, Version: "v1.0.0"}
	err := info.Render(errWriter{})
	if err == nil {
		t.Fatal("Render swallowed the writer error")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestPseudoVersionShapes(t *testing.T) {
	// The timestamp+revision suffix is what all pseudo-version forms share;
	// the prefix form differs but classification must hold.
	for _, v := range []string{
		"v0.0.0-20261104112648-3996d08e0e87",
		"v1.2.3-pre.0.20261104112648-3996d08e0e87",
		"v1.2.4-0.20261104112648-3996d08e0e87-abcdefabcdef",
		// Build metadata suffixes do not hide the pseudo-version tail.
		"v0.0.0-20261104145922-1671e67be376+dirty",
	} {
		if !isPseudoVersion(v) {
			t.Errorf("isPseudoVersion(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"v1.2.3", "v1.2.3-rc.1", "(devel)", "", "v1.2.3-202611041126-abcdefghabcd"} {
		if isPseudoVersion(v) {
			t.Errorf("isPseudoVersion(%q) = true, want false", v)
		}
	}
}
