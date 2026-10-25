package project

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Markers delimiting the za-managed project routing region of AGENTS.md.
const (
	routesStart = "<!-- za:projects:start -->"
	routesEnd   = "<!-- za:projects:end -->"

	noRoutes     = "_No projects registered._"
	routesHeader = "| Project ID | Canonical checkout | Private index |"
	routesRule   = "|---|---|---|"
)

var routeRow = regexp.MustCompile("^\\| `([^`]+)` \\| `([^`]+)/` \\| `\\.agents/context/projects/([^`]+)/INDEX\\.md` \\|$")

func routeLine(id string) string {
	return fmt.Sprintf("| `%s` | `%s/` | `.agents/context/projects/%s/INDEX.md` |", id, id, id)
}

// addRoute returns agents with a route to id added to the managed region.
// Everything outside the region is preserved byte for byte. It fails rather
// than guessing when the region is missing, duplicated, out of order, or
// holds content za did not write.
func addRoute(agents []byte, id string) ([]byte, error) {
	s := string(agents)
	if err := requireMarkerLine(s, routesStart); err != nil {
		return nil, err
	}
	if err := requireMarkerLine(s, routesEnd); err != nil {
		return nil, err
	}
	startLine := strings.Index(s, routesStart)
	endLine := strings.Index(s, routesEnd)
	if endLine < startLine {
		return nil, errors.New("AGENTS.md: project routing end marker precedes the start marker")
	}
	bodyStart := startLine + len(routesStart) + 1 // past the marker's newline
	body := s[bodyStart:endLine]

	ids, err := parseRoutes(body)
	if err != nil {
		return nil, err
	}
	if slices.Contains(ids, id) {
		return nil, fmt.Errorf("AGENTS.md already routes project %q", id)
	}
	ids = append(ids, id)
	slices.Sort(ids)

	var b strings.Builder
	b.WriteString(s[:bodyStart])
	b.WriteString(routesHeader + "\n" + routesRule + "\n")
	for _, p := range ids {
		b.WriteString(routeLine(p))
		b.WriteByte('\n')
	}
	b.WriteString(s[endLine:])
	return []byte(b.String()), nil
}

// requireMarkerLine checks that marker occurs exactly once, alone on its line.
func requireMarkerLine(s, marker string) error {
	switch n := strings.Count(s, marker); n {
	case 0:
		return fmt.Errorf("AGENTS.md: missing project routing marker %s", marker)
	case 1:
	default:
		return fmt.Errorf("AGENTS.md: project routing marker %s appears %d times", marker, n)
	}
	i := strings.Index(s, marker)
	before := i == 0 || s[i-1] == '\n'
	after := strings.HasPrefix(s[i+len(marker):], "\n")
	if !before || !after {
		return fmt.Errorf("AGENTS.md: project routing marker %s must be alone on its own line", marker)
	}
	return nil
}

// parseRoutes returns the project IDs of a managed region body in the order
// they appear. The body must be empty, the placeholder, or a za route table.
func parseRoutes(body string) ([]string, error) {
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if body == "" || (len(lines) == 1 && lines[0] == noRoutes) {
		return nil, nil
	}
	malformed := errors.New("AGENTS.md: the project routing region holds content za did not generate; fix it manually")
	if len(lines) < 2 || lines[0] != routesHeader || lines[1] != routesRule {
		return nil, malformed
	}
	var ids []string
	for _, line := range lines[2:] {
		m := routeRow.FindStringSubmatch(line)
		if m == nil || m[1] != m[2] || m[1] != m[3] || validateID(m[1]) != nil {
			return nil, malformed
		}
		if slices.Contains(ids, m[1]) {
			return nil, fmt.Errorf("AGENTS.md: project %q is routed twice", m[1])
		}
		ids = append(ids, m[1])
	}
	return ids, nil
}
