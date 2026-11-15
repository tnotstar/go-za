package issue

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Issue
	}{
		{
			name: "active without priority",
			line: "2026-11-15 Fix the parser +go-za @todo id:I-000001",
			want: Issue{ID: "I-000001", Status: Todo, Created: "2026-11-15", Title: "Fix the parser", Projects: []string{"go-za"}},
		},
		{
			name: "active with priority",
			line: "(A) 2026-11-04 go-za follow-up directions from bootstrap reconciliation +go-za @todo id:I-000001",
			want: Issue{ID: "I-000001", Status: Todo, Priority: 'A', Created: "2026-11-04", Title: "go-za follow-up directions from bootstrap reconciliation", Projects: []string{"go-za"}},
		},
		{
			name: "doing with several projects and links",
			line: "(B) 2026-11-11 Support status.log change journals and .za/ workflow in za +go-za +workspace @doing id:I-000002 spec:.za/wiki/index.md adr:D-0001",
			want: Issue{ID: "I-000002", Status: Doing, Priority: 'B', Created: "2026-11-11", Title: "Support status.log change journals and .za/ workflow in za", Projects: []string{"go-za", "workspace"}, Spec: ".za/wiki/index.md", ADR: "D-0001"},
		},
		{
			name: "done completed form",
			line: "x 2026-11-15 2026-11-11 (B) Support status.log change journals and .za/ workflow in za +go-za @done id:I-000002 commit:5c35090",
			want: Issue{ID: "I-000002", Status: Done, Priority: 'B', Created: "2026-11-11", Completed: "2026-11-15", Title: "Support status.log change journals and .za/ workflow in za", Projects: []string{"go-za"}, Commit: "5c35090"},
		},
		{
			name: "workspace-only issue without projects",
			line: "2026-11-15 Title only @doing id:I-000003",
			want: Issue{ID: "I-000003", Status: Doing, Created: "2026-11-15", Title: "Title only"},
		},
		{
			name: "unknown key:value tags are preserved",
			line: "2026-11-15 Title +p @todo id:I-000004 spec:x.md team:core",
			want: Issue{ID: "I-000004", Status: Todo, Created: "2026-11-15", Title: "Title", Projects: []string{"p"}, Spec: "x.md", Extra: []string{"team:core"}},
		},
		{
			name: "a tag-shaped word before the trailing tag region stays in the title",
			line: "2026-11-15 Fix +go-za integration and @todo handling id:I-000005 @doing",
			want: Issue{ID: "I-000005", Status: Doing, Created: "2026-11-15", Title: "Fix +go-za integration and @todo handling"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.line)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !equalIssue(got, tt.want) {
				t.Errorf("Parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func equalIssue(a, b Issue) bool {
	if a.ID != b.ID || a.Status != b.Status || a.Priority != b.Priority || a.Created != b.Created ||
		a.Completed != b.Completed || a.Title != b.Title || a.Spec != b.Spec || a.ADR != b.ADR || a.Commit != b.Commit {
		return false
	}
	if strings.Join(a.Projects, ",") != strings.Join(b.Projects, ",") ||
		strings.Join(a.Extra, ",") != strings.Join(b.Extra, ",") {
		return false
	}
	return true
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		line string
		err  string
	}{
		{name: "empty", line: "", err: "empty line"},
		{name: "only spaces", line: "   ", err: "empty line"},
		{name: "missing status", line: "2026-11-15 Title +p id:I-000001", err: "missing status tag"},
		{name: "unknown status", line: "2026-11-15 Title +p @blocked id:I-000001", err: "unknown status tag"},
		{name: "two status tags", line: "2026-11-15 Title @todo @doing id:I-000001", err: "multiple status tags"},
		{name: "missing id", line: "2026-11-15 Title +p @todo", err: "missing id:I-NNNNNN tag"},
		{name: "malformed id", line: "2026-11-15 Title +p @todo id:4", err: "malformed id tag"},
		{name: "two id tags", line: "2026-11-15 Title @todo id:I-000001 id:I-000002", err: "multiple id tags"},
		{name: "missing date", line: "Title +p @todo id:I-000001", err: "creation date"},
		{name: "malformed date", line: "2026-11-5 Title @todo id:I-000001", err: "creation date"},
		{name: "impossible date", line: "2026-02-30 Title @todo id:I-000001", err: "creation date"},
		{name: "done without x", line: "2026-11-15 Title @done id:I-000001", err: `@done requires the completed "x" line form`},
		{name: "x with todo", line: "x 2026-11-15 2026-11-14 Title @todo id:I-000001", err: `instead of @done`},
		{name: "x without dates", line: "x Title @done id:I-000001", err: `malformed completion date "Title"`},
		{name: "x alone", line: "x", err: "lacks the completion and creation dates"},
		{name: "missing title", line: "2026-11-15 @todo id:I-000001", err: "missing title"},
		{name: "malformed project tag", line: "2026-11-15 Title + @todo id:I-000001", err: "malformed project tag"},
		{name: "empty tag value", line: "2026-11-15 Title +p @todo id:I-000001 spec:", err: "empty value"},
		{name: "id does not fit the shape", line: "2026-11-15 Title @todo id:I-4", err: "malformed id tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.line)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("Parse error = %v, want containing %q", err, tt.err)
			}
		})
	}
}

func TestSerializeCanonical(t *testing.T) {
	tests := []struct {
		name  string
		issue Issue
		want  string
	}{
		{
			name:  "active canonical order",
			issue: Issue{ID: "I-000001", Status: Todo, Priority: 'B', Created: "2026-11-04", Title: "Title line", Projects: []string{"go-za"}, Spec: ".za/wiki/index.md", ADR: "D-0001"},
			want:  "(B) 2026-11-04 Title line +go-za @todo id:I-000001 spec:.za/wiki/index.md adr:D-0001",
		},
		{
			name:  "done canonical order",
			issue: Issue{ID: "I-000002", Status: Done, Priority: 'B', Created: "2026-11-11", Completed: "2026-11-15", Title: "Done work", Projects: []string{"go-za"}, Commit: "5c35090"},
			want:  "x 2026-11-15 2026-11-11 (B) Done work +go-za @done id:I-000002 commit:5c35090",
		},
		{
			name:  "unprioritized with extra tags",
			issue: Issue{ID: "I-000003", Status: Doing, Created: "2026-11-15", Title: "Work", Projects: []string{"a", "b"}, Extra: []string{"team:core"}},
			want:  "2026-11-15 Work +a +b @doing id:I-000003 team:core",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Serialize(tt.issue); got != tt.want {
				t.Errorf("Serialize =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	lines := []string{
		"(A) 2026-11-15 Implement .za/ workspace scaffolding, issue/kanban CLI, and wiki CLI in go-za +go-za @doing id:I-000004",
		"(B) 2026-11-04 go-za follow-up directions from bootstrap reconciliation +go-za @todo id:I-000001",
		"x 2026-11-15 2026-11-11 (B) Support status.log change journals and .za/ workflow in za +go-za @done id:I-000002 commit:5c35090",
		"(C) 2026-11-15 Implement additional sqlt database adapters and structured logging filters +go-minolas @todo id:I-000003",
		"2026-11-15 Unprioritized workspace note @todo id:I-000005 spec:.za/wiki/go-minolas-architecture.md",
	}
	for _, line := range lines {
		first, err := Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q): %v", line, err)
		}
		second, err := Parse(Serialize(first))
		if err != nil {
			t.Fatalf("reparse: %v", err)
		}
		if !equalIssue(first, second) {
			t.Errorf("round trip changed the record:\n%+v\n%+v", first, second)
		}
	}
}

func TestParseAllocating(t *testing.T) {
	iss, err := ParseAllocating("(A) 2026-11-15 Raw line +go-za @todo")
	if err != nil {
		t.Fatalf("ParseAllocating: %v", err)
	}
	if iss.ID != "" || iss.Title != "Raw line" || iss.Status != Todo || iss.Priority != 'A' {
		t.Errorf("unexpected record %+v", iss)
	}
	if _, err := ParseAllocating("Just a plain title"); err == nil {
		t.Error("ParseAllocating accepted a plain title without a status tag")
	}
}

func TestNormalizeID(t *testing.T) {
	valid := map[string]string{
		"I-000004": "I-000004",
		"4":        "I-000004",
		"000004":   "I-000004",
		"123456":   "I-123456",
		"999999":   "I-999999",
	}
	for in, want := range valid {
		got, err := NormalizeID(in)
		if err != nil || got != want {
			t.Errorf("NormalizeID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "I-4", "i-000004", "1234567", "abc", "-4", "+4", "I-00000 "} {
		if got, err := NormalizeID(in); err == nil {
			t.Errorf("NormalizeID(%q) = %q, want an error", in, got)
		}
	}
}

var testNow = time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)
