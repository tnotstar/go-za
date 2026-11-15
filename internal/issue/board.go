package issue

import (
	"fmt"
	"slices"
	"strings"
)

// Board renders the deterministic plain-text Kanban view: one section per
// state (@doing, @todo, @done), each row showing ID, priority, projects and
// title, sorted by priority (A..Z, unprioritized last) and then by ID. The
// output carries no ANSI escapes, so it is safe for every terminal and for
// byte-comparing tests.
func Board(issues []Issue) string {
	groups := map[Status][]Issue{}
	for _, iss := range issues {
		groups[iss.Status] = append(groups[iss.Status], iss)
	}
	var sections []string
	for _, st := range []Status{Doing, Todo, Done} {
		var b strings.Builder
		fmt.Fprintf(&b, "%s\n", strings.ToUpper(string(st)))
		for _, iss := range sorted(groups[st]) {
			fmt.Fprintf(&b, "  %s\n", row(iss))
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n")
}

func row(iss Issue) string {
	prio := "-"
	if iss.Priority != 0 {
		prio = "(" + string(iss.Priority) + ")"
	}
	projects := "-"
	if len(iss.Projects) > 0 {
		projects = "+" + strings.Join(iss.Projects, " +")
	}
	return fmt.Sprintf("%s  %s  %s  %s", iss.ID, prio, projects, iss.Title)
}

// Filter applies the list and board filters: status is "active" (@todo and
// @doing, the default view), one state word or "all"; an empty project and
// a 0 priority keep everything.
func Filter(issues []Issue, status, project string, priority byte) []Issue {
	return slices.DeleteFunc(slices.Clone(issues), func(iss Issue) bool {
		switch status {
		case "", "all":
		case "active":
			if iss.Status == Done {
				return true
			}
		case string(iss.Status):
		default:
			return true
		}
		if project != "" && !slices.Contains(iss.Projects, project) {
			return true
		}
		if priority != 0 && iss.Priority != priority {
			return true
		}
		return false
	})
}

// RenderList renders the tab-separated single-line records of
// "za issue ls": ID, status, priority, created date, projects and title,
// with "-" for an absent priority or project list.
func RenderList(issues []Issue) string {
	var b strings.Builder
	for _, iss := range issues {
		prio := "-"
		if iss.Priority != 0 {
			prio = "(" + string(iss.Priority) + ")"
		}
		projects := "-"
		if len(iss.Projects) > 0 {
			projects = strings.Join(iss.Projects, ",")
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", iss.ID, iss.Status, prio, iss.Created, projects, iss.Title)
	}
	return b.String()
}

// sorted orders issues by priority (A..Z, unprioritized last) and then by
// ascending ID.
func sorted(issues []Issue) []Issue {
	out := slices.Clone(issues)
	slices.SortFunc(out, func(a, b Issue) int {
		if c := rankPriority(a.Priority) - rankPriority(b.Priority); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// SortByPriorityStatusID orders issues for "za issue ls": priority (A..Z,
// unprioritized last), then status (@doing before @todo before @done),
// then ascending ID.
func SortByPriorityStatusID(issues []Issue) {
	slices.SortFunc(issues, func(a, b Issue) int {
		if c := rankPriority(a.Priority) - rankPriority(b.Priority); c != 0 {
			return c
		}
		if c := rankStatus(a.Status) - rankStatus(b.Status); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

func rankPriority(p byte) int {
	if p == 0 {
		return 'Z' - 'A' + 1 // unprioritized sorts after every letter
	}
	return int(p - 'A')
}

func rankStatus(s Status) int {
	switch s {
	case Doing:
		return 0
	case Todo:
		return 1
	default:
		return 2
	}
}
