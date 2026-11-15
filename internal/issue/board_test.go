package issue

import (
	"strings"
	"testing"
)

func boardFixture() []Issue {
	return []Issue{
		{ID: "I-000001", Status: Todo, Priority: 'B', Created: "2026-11-04", Title: "Follow-up directions", Projects: []string{"go-za"}},
		{ID: "I-000002", Status: Doing, Priority: 'B', Created: "2026-11-11", Title: "Support status.log journals", Projects: []string{"go-za"}},
		{ID: "I-000003", Status: Todo, Priority: 'C', Created: "2026-11-15", Title: "sqlt adapters", Projects: []string{"go-minolas"}},
		{ID: "I-000004", Status: Doing, Priority: 'A', Created: "2026-11-15", Title: "Issue CLI", Projects: []string{"go-za"}},
		{ID: "I-000005", Status: Done, Created: "2026-11-01", Completed: "2026-11-02", Title: "Landed work"},
	}
}

func TestBoard(t *testing.T) {
	want := "DOING\n" +
		"  I-000004  (A)  +go-za  Issue CLI\n" +
		"  I-000002  (B)  +go-za  Support status.log journals\n" +
		"\n" +
		"TODO\n" +
		"  I-000001  (B)  +go-za  Follow-up directions\n" +
		"  I-000003  (C)  +go-minolas  sqlt adapters\n" +
		"\n" +
		"DONE\n" +
		"  I-000005  -  -  Landed work\n"
	if got := Board(boardFixture()); got != want {
		t.Errorf("Board =\n%s\nwant\n%s", got, want)
	}
}

func TestBoardEmptyGroupsStayDeterministic(t *testing.T) {
	want := "DOING\n\nTODO\n\nDONE\n"
	if got := Board(nil); got != want {
		t.Errorf("Board(nil) = %q, want %q", got, want)
	}
}

func TestSortByPriorityStatusID(t *testing.T) {
	issues := []Issue{
		{ID: "I-000002", Status: Todo, Priority: 'A'},
		{ID: "I-000003", Status: Done, Priority: 'A'},
		{ID: "I-000001", Status: Doing, Priority: 'A'},
		{ID: "I-000010", Status: Todo, Priority: 0},
		{ID: "I-000004", Status: Todo, Priority: 'B'},
		{ID: "I-000005", Status: Todo, Priority: 0},
	}
	SortByPriorityStatusID(issues)
	var ids []string
	for _, iss := range issues {
		ids = append(ids, iss.ID)
	}
	want := []string{"I-000001", "I-000002", "I-000003", "I-000004", "I-000005", "I-000010"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

func TestFilter(t *testing.T) {
	issues := boardFixture()
	if got := Filter(issues, "active", "", 0); len(got) != 4 {
		t.Errorf("active filter kept %d issues, want 4", len(got))
	}
	if got := Filter(issues, "done", "", 0); len(got) != 1 || got[0].ID != "I-000005" {
		t.Errorf("done filter = %+v", got)
	}
	if got := Filter(issues, "all", "go-minolas", 0); len(got) != 1 || got[0].ID != "I-000003" {
		t.Errorf("project filter = %+v", got)
	}
	if got := Filter(issues, "all", "", 'B'); len(got) != 2 {
		t.Errorf("priority filter kept %d issues, want 2", len(got))
	}
	if got := Filter(issues, "all", "", 0); len(got) != 5 {
		t.Errorf("all filter kept %d issues, want 5", len(got))
	}
	if got := Filter(issues, "bogus", "", 0); len(got) != 0 {
		t.Errorf("an unknown status filter must keep nothing, got %d", len(got))
	}
	// The filter must not mutate its input.
	if len(issues) != 5 {
		t.Errorf("Filter mutated its input: %d issues left", len(issues))
	}
}

func TestRenderList(t *testing.T) {
	// RenderList preserves the caller's order; the CLI sorts before rendering.
	want := "I-000005\tdone\t-\t2026-11-01\t-\tLanded work\n" +
		"I-000004\tdoing\t(A)\t2026-11-15\tgo-za\tIssue CLI\n"
	got := RenderList([]Issue{
		{ID: "I-000005", Status: Done, Created: "2026-11-01", Completed: "2026-11-02", Title: "Landed work"},
		{ID: "I-000004", Status: Doing, Priority: 'A', Created: "2026-11-15", Title: "Issue CLI", Projects: []string{"go-za"}},
	})
	if got != want {
		t.Errorf("RenderList =\n%s\nwant\n%s", got, want)
	}
}
