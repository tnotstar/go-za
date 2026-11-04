// Package version derives the za program version from the Go runtime build
// information embedded in the running executable.
//
// There is exactly one source of version truth: the toolchain stamps the main
// module version (Main.Version) and the VCS state (vcs.revision, vcs.time,
// vcs.modified) at link time. za declares no build-time injection mechanism
// and keeps no version constant in the source, so go build, make build,
// make install and go install module@version all report without special
// preparation, and the recorded values cannot drift apart. When the metadata
// is absent — a build without build information, outside a VCS checkout, or
// with -buildvcs=false — it is reported as explicitly unavailable, never
// invented.
package version

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// Kind classifies a build by its main module version.
type Kind int

const (
	// Unknown means no usable version classification: the executable has
	// no build information, an empty main module version, or a version
	// that is neither a semantic version nor a Go pseudo-version. The
	// literal version string, when present, is still reported.
	Unknown Kind = iota
	// Development means the executable was built from a local checkout:
	// the main module version is "(devel)", as produced by go build,
	// make build and make install in a module directory.
	Development
	// PseudoVersion means the main module version is a Go pseudo-version,
	// as recorded by go install module@<branch-or-commit> without a tag.
	PseudoVersion
	// Release means the main module version is a tagged semantic version.
	// Release identifiers use semantic version syntax.
	Release
)

// Info is the build identity observed in an executable.
type Info struct {
	Kind       Kind
	Version    string // main module version; "unknown" when absent
	Module     string // main module path; empty when unavailable
	Revision   string // vcs.revision; empty when unavailable
	Modified   bool   // vcs.modified is "true"
	CommitTime string // vcs.time; empty when unavailable
	GoVersion  string // Go toolchain that produced the executable
}

// Read returns the build information of the running executable, or nil if it
// is unavailable.
func Read() *debug.BuildInfo {
	bi, _ := debug.ReadBuildInfo()
	return bi
}

// Inspect extracts Info from bi, tolerating nil and missing fields. Every
// unavailable fact keeps its empty/conservative representation; nothing is
// guessed.
func Inspect(bi *debug.BuildInfo) Info {
	info := Info{Kind: Unknown, Version: "unknown"}
	if bi == nil {
		return info
	}
	info.GoVersion = bi.GoVersion
	info.Module = bi.Main.Path
	v := bi.Main.Version
	if v == "" {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		case "vcs.time":
			info.CommitTime = s.Value
		}
	}
	info.Version = v
	switch {
	// Modified source is a development build however the toolchain named
	// its version: current Go stamps local builds as a pseudo-version of
	// HEAD with a "+dirty" suffix rather than the literal "(devel)".
	case info.Modified, v == "(devel)":
		info.Kind = Development
	case isPseudoVersion(v):
		info.Kind = PseudoVersion
	case isSemanticVersion(v):
		info.Kind = Release
	}
	return info
}

// Render writes the human-readable version report: one summary line naming
// the version and its kind, followed by one detail line per available fact.
func (i Info) Render(w io.Writer) error {
	p := func(format string, a ...any) error {
		_, err := fmt.Fprintf(w, format, a...)
		return err
	}
	switch i.Kind {
	case Development:
		if err := p("za %s (development build)\n", i.Version); err != nil {
			return err
		}
	case PseudoVersion:
		if err := p("za %s (Go pseudo-version)\n", i.Version); err != nil {
			return err
		}
	case Release:
		if err := p("za %s (release)\n", i.Version); err != nil {
			return err
		}
	case Unknown:
		if i.Version == "unknown" {
			_, err := fmt.Fprintln(w, "za unknown (no build metadata)")
			return err
		}
		return p("za %s (unrecognized build version)\n", i.Version)
	}
	if i.Module != "" {
		if err := p("  module:    %s\n", i.Module); err != nil {
			return err
		}
	}
	switch {
	case i.Revision != "" && i.Modified:
		if err := p("  revision:  %s (modified)\n", i.Revision); err != nil {
			return err
		}
	case i.Revision != "":
		if err := p("  revision:  %s (clean)\n", i.Revision); err != nil {
			return err
		}
	default:
		if err := p("  revision:  unavailable (no VCS metadata stamped at build time)\n"); err != nil {
			return err
		}
	}
	if i.CommitTime != "" {
		if err := p("  committed: %s\n", i.CommitTime); err != nil {
			return err
		}
	}
	if i.GoVersion != "" {
		if err := p("  go:        %s\n", i.GoVersion); err != nil {
			return err
		}
	}
	return nil
}

// isPseudoVersion reports whether v carries a Go pseudo-version suffix. All
// pseudo-version forms share the same tail: a 14-digit UTC timestamp followed
// by one or two 12-character lowercase-hex commit prefixes. The timestamp is
// either a segment of its own (vX.0.0-...) or the tail of a dotted segment
// (vX.Y.Z-pre.0.-... and the folded vX.Y.(Z+1)-0.-... form).
func isPseudoVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	// Build metadata such as "+dirty" and "+incompatible" rides on the last
	// segment; strip it before examining the pseudo-version tail.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	segs := strings.Split(v, "-")
	if len(segs) < 3 {
		return false
	}
	n := len(segs)
	if !isCommitPrefix(segs[n-1]) {
		return false
	}
	if isCommitPrefix(segs[n-2]) {
		// Two commit prefixes: the folded form, where the timestamp sits
		// one segment further left.
		if n < 4 {
			return false
		}
		return endsWithTimestamp(segs[n-3])
	}
	return endsWithTimestamp(segs[n-2])
}

// endsWithTimestamp reports whether s ends, after an optional dotted prefix,
// with a 14-digit timestamp.
func endsWithTimestamp(s string) bool {
	t := s[strings.LastIndexByte(s, '.')+1:]
	return len(t) == 14 && isDigits(t)
}

// isCommitPrefix reports whether s is a 12-character lowercase-hex commit
// prefix.
func isCommitPrefix(s string) bool {
	return len(s) == 12 && isLowerHex(s)
}

// isSemanticVersion reports whether v is a semantic version: "v" followed by
// three numeric components, an optional -prerelease and an optional +build
// suffix.
func isSemanticVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	rest := strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		rest = rest[:i] // build metadata does not affect classification
	}
	core := rest
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		core = rest[:i]
	}
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return false
	}
	for _, n := range nums {
		if n == "" || !isDigits(n) {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return s != ""
}
