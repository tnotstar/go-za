package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeIntegrationWorkspace(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{
			name: "appends tables after uv metadata",
			in:   "[project]\nname = \"ws\"\nversion = \"0.1.0\"\n",
			want: "[project]\nname = \"ws\"\nversion = \"0.1.0\"\n" + integrationTables,
		},
		{
			name: "normalizes missing and extra trailing newlines",
			in:   "[project]\nname = \"ws\"\n\n\n",
			want: "[project]\nname = \"ws\"\n" + integrationTables,
		},
		{name: "missing project table", in: "name = \"ws\"\n", wantErr: "no [project] table"},
		{name: "existing tool.uv table", in: "[project]\n[tool.uv]\npackage = true\n", wantErr: "refusing to edit"},
		{name: "existing tool table", in: "[project]\n[tool]\nuv = {}\n", wantErr: "refusing to edit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pyproject.toml")
			if err := os.WriteFile(path, []byte(tt.in), 0o644); err != nil {
				t.Fatal(err)
			}
			err := makeIntegrationWorkspace(path)
			got, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if string(got) != tt.in {
					t.Errorf("file modified on error:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("content =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
