package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unrelatedTools holds [tool.*] tables outside the uv namespace. They must
// survive augmentation unchanged; rejecting them is the overly broad rule
// this package used to apply to every "[tool" header.
const unrelatedTools = `[project]
name = "ws"
version = "0.1.0"

[tool.example]
enabled = true

[tool.ruff]
line-length = 100

[tool.pytest.ini_options]
addopts = "-q"

[tool.uvicorn]
reload = true
`

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
		{
			name: "preserves unrelated tool tables",
			in:   unrelatedTools,
			want: unrelatedTools + integrationTables,
		},
		{
			name: "preserves a tool table without uv keys",
			in:   "[project]\nname = \"ws\"\n\n[tool]\nexample = 1\n",
			want: "[project]\nname = \"ws\"\n\n[tool]\nexample = 1\n" + integrationTables,
		},
		{
			name: "array elements are not table headers",
			in:   "[project]\nname = \"ws\"\nclassifiers = [\n  [\"tool.uv\"],\n]\n",
			want: "[project]\nname = \"ws\"\nclassifiers = [\n  [\"tool.uv\"],\n]\n" + integrationTables,
		},
		{name: "missing project table", in: "name = \"ws\"\n", wantErr: "no [project] table"},
		{name: "unrelated tool table without project", in: "[tool.example]\nenabled = true\n", wantErr: "no [project] table"},
		{name: "existing tool.uv table", in: "[project]\n[tool.uv]\npackage = true\n", wantErr: "defines [tool.uv]"},
		{name: "existing tool.uv.workspace table", in: "[project]\n[tool.uv.workspace]\nmembers = [\"a\"]\n", wantErr: "defines [tool.uv.workspace]"},
		{name: "other tool.uv sub-table", in: "[project]\n[tool.uv.sources]\nfoo = { workspace = true }\n", wantErr: "defines [tool.uv.sources]"},
		{name: "tool.uv array of tables", in: "[project]\n[[tool.uv.index]]\nurl = \"x\"\n", wantErr: "defines [tool.uv.index]"},
		{name: "header with spaces, quotes and comment", in: "[project]\n[ tool . \"uv\" ]  # mine\n", wantErr: `defines [tool . "uv"]`},
		{name: "uv key in tool table", in: "[project]\n[tool]\nuv = {}\n", wantErr: `key "uv" in [tool]`},
		{name: "dotted uv key in tool table", in: "[project]\n[tool]\nuv.package = true\n", wantErr: `key "uv.package" in [tool]`},
		{name: "dotted tool.uv key at root", in: "tool.uv.package = true\n[project]\n", wantErr: `key "tool.uv.package" in the root table`},
		{name: "inline tool table at root", in: "tool = { example = 1 }\n[project]\n", wantErr: `key "tool" in the root table`},
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
