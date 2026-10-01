package initcmd

import (
	"errors"
	"flag"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Options
		wantErr bool
	}{
		{name: "go only", args: []string{"--go"}, want: Options{Go: true}},
		{name: "python only", args: []string{"--python"}, want: Options{Python: true}},
		{name: "polyglot", args: []string{"--go", "--python"}, want: Options{Go: true, Python: true}},
		{name: "single dash flags", args: []string{"-go", "-python"}, want: Options{Go: true, Python: true}},
		{name: "path after flags", args: []string{"--go", "./ws"}, want: Options{Go: true, Path: "./ws"}},
		{name: "path before flags", args: []string{"./ws", "--python"}, want: Options{Python: true, Path: "./ws"}},
		{name: "path between flags", args: []string{"--go", "../p", "--python"}, want: Options{Go: true, Python: true, Path: "../p"}},
		{name: "dash-prefixed path after separator", args: []string{"--go", "--", "--python"}, want: Options{Go: true, Path: "--python"}},
		{name: "separator without path", args: []string{"--go", "--"}, want: Options{Go: true}},
		{name: "explicit true value", args: []string{"--go=true"}, want: Options{Go: true}},
		{name: "separator after path", args: []string{"--go", "a", "--", "b"}, wantErr: true},
		{name: "no workspace type", args: nil, wantErr: true},
		{name: "path without workspace type", args: []string{"./ws"}, wantErr: true},
		{name: "both explicitly false", args: []string{"--go=false", "--python=false"}, wantErr: true},
		{name: "two paths", args: []string{"--go", "a", "b"}, wantErr: true},
		{name: "two paths around flag", args: []string{"a", "--go", "b"}, wantErr: true},
		{name: "unknown option", args: []string{"--go", "--rust"}, wantErr: true},
		{name: "empty path", args: []string{"--go", ""}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseArgs(tt.args)
			if tt.wantErr {
				var usage *UsageError
				if !errors.As(err, &usage) {
					t.Fatalf("ParseArgs(%q) error = %v, want *UsageError", tt.args, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%q) unexpected error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("ParseArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--go", "--help"}} {
		if _, err := ParseArgs(args); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("ParseArgs(%q) error = %v, want flag.ErrHelp", args, err)
		}
	}
}

func TestMissingWorkspaceTypeMessage(t *testing.T) {
	_, err := ParseArgs(nil)
	want := "at least one workspace type is required: --go and/or --python"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}
