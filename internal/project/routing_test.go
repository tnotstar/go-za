package project

import (
	"strings"
	"testing"
)

const (
	before = "# Router\n\nUser text before.\n\n## Projects\n\n"
	after  = "\n## Loading protocol\n\nUser text after.\n"
)

func region(body string) string {
	return routesStart + "\n" + body + routesEnd + "\n"
}

func TestAddRouteFirstProject(t *testing.T) {
	in := before + region(noRoutes+"\n") + after
	got, err := addRoute([]byte(in), "go-za")
	if err != nil {
		t.Fatal(err)
	}
	want := before + region(routesHeader+"\n"+routesRule+"\n"+
		"| `go-za` | `go-za/` | `.agents/context/projects/go-za/INDEX.md` |\n") + after
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddRouteSortsExistingProjects(t *testing.T) {
	table := routesHeader + "\n" + routesRule + "\n" + routeLine("zaz") + "\n" + routeLine("bar") + "\n"
	got, err := addRoute([]byte(before+region(table)+after), "foo")
	if err != nil {
		t.Fatal(err)
	}
	want := before + region(routesHeader+"\n"+routesRule+"\n"+
		routeLine("bar")+"\n"+routeLine("foo")+"\n"+routeLine("zaz")+"\n") + after
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddRouteEmptyRegion(t *testing.T) {
	got, err := addRoute([]byte(region("")), "a")
	if err != nil {
		t.Fatal(err)
	}
	if want := region(routesHeader + "\n" + routesRule + "\n" + routeLine("a") + "\n"); string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAddRouteRejects(t *testing.T) {
	table := routesHeader + "\n" + routesRule + "\n" + routeLine("foo") + "\n"
	tests := []struct {
		name, in, err string
	}{
		{"missing start", before + noRoutes + "\n" + routesEnd + "\n" + after, "missing project routing marker " + routesStart},
		{"missing end", before + routesStart + "\n" + noRoutes + "\n" + after, "missing project routing marker " + routesEnd},
		{"duplicate start", region(noRoutes+"\n") + routesStart + "\n", "appears 2 times"},
		{"duplicate region", region(noRoutes+"\n") + region(noRoutes+"\n"), "appears 2 times"},
		{"reversed", routesEnd + "\n" + noRoutes + "\n" + routesStart + "\n", "precedes"},
		{"marker not on own line", "x " + routesStart + "\n" + routesEnd + "\n", "alone on its own line"},
		{"end marker inline", routesStart + "\n" + noRoutes + routesEnd + "\n", "alone on its own line"},
		{"foreign content", region("- my notes\n"), "did not generate"},
		{"foreign row", region(table + "| `x` | `y/` | `.agents/context/projects/x/INDEX.md` |\n"), "did not generate"},
		{"duplicate row", region(table + routeLine("foo") + "\n"), "routed twice"},
		{"already routed", region(table), "already routes project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := addRoute([]byte(tt.in), "foo")
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("err = %v, want containing %q", err, tt.err)
			}
		})
	}
}
