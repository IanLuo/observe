package analyze

import (
	"testing"
)

// fixture is a small project shaped like observe: an entry point, two internal
// packages with a dependency between them, a TypeScript UI, and a test file.
func fixture() []File {
	return []File{
		{Path: "cmd/tool/main.go", Body: []byte(`package main

import (
	"fmt"

	"observe/internal/a"
)

func main() { fmt.Println(a.A()) }
`)},
		{Path: "internal/a/a.go", Body: []byte(`package a

import (
	"net/http"

	"observe/internal/b"
)

func A() string { return http.MethodGet + b.B() }
`)},
		{Path: "internal/a/a_test.go", Body: []byte(`package a

import "testing"

func TestA(t *testing.T) { _ = A() }
`)},
		{Path: "internal/b/b.go", Body: []byte(`package b

func B() string { return "b" }
`)},
		{Path: "web/main.ts", Body: []byte(`import { render } from "./render";

export function start(): void { render(); }
`)},
		{Path: "web/render.ts", Body: []byte(`export function render(): void {}
`)},
		{Path: "README.md", Body: []byte("# not source\n")},
	}
}

// The project's shape is modules and the imports between them. This is what the
// map draws: not the goals, the construction.
func TestStructureOfMapsModulesAndTheirDependencies(t *testing.T) {
	structure, diags := StructureOf(fixture())
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	want := []string{"cmd/tool", "internal/a", "internal/b", "web"}
	if len(structure.Modules) != len(want) {
		t.Fatalf("modules = %d (%+v), want %d", len(structure.Modules), structure.Modules, len(want))
	}
	for i, dir := range want {
		if structure.Modules[i].Dir != dir {
			t.Fatalf("module %d = %q, want %q", i, structure.Modules[i].Dir, dir)
		}
	}

	a := module(t, structure, "internal/a")
	if len(a.DependsOn) != 1 || a.DependsOn[0] != "internal/b" {
		t.Errorf("internal/a depends on %v, want [internal/b]", a.DependsOn)
	}
	if a.Files != 2 || a.TestFiles != 1 {
		t.Errorf("internal/a files = %d/%d test, want 2/1", a.Files, a.TestFiles)
	}
	if a.Decls != 1 || a.TestDecls != 1 {
		t.Errorf("internal/a decls = %d/%d test, want 1/1", a.Decls, a.TestDecls)
	}

	// An import that names no module here is a real dependency, but not an edge:
	// calling it external keeps the diagram honest instead of guessing a target.
	// "testing" is absent because test files are counted, not wired.
	if len(a.Imports) != 1 || a.Imports[0] != "net/http" {
		t.Errorf("internal/a external imports = %v, want [net/http]", a.Imports)
	}

	// A relative TypeScript import names a file inside its own module, so it is
	// neither a module edge nor an external dependency: the UI is one module.
	web := module(t, structure, "web")
	if len(web.DependsOn) != 0 || len(web.Imports) != 0 {
		t.Errorf("web depends on %v / imports %v, want neither", web.DependsOn, web.Imports)
	}
	if web.Files != 2 || web.Decls != 2 {
		t.Errorf("web = %d files, %d decls, want 2/2", web.Files, web.Decls)
	}

	wantEdges := []ModuleEdge{
		{From: "cmd/tool", To: "internal/a", Import: "observe/internal/a"},
		{From: "internal/a", To: "internal/b", Import: "observe/internal/b"},
	}
	if len(structure.Edges) != len(wantEdges) {
		t.Fatalf("edges = %+v, want %+v", structure.Edges, wantEdges)
	}
	for i, edge := range wantEdges {
		if structure.Edges[i] != edge {
			t.Errorf("edge %d = %+v, want %+v", i, structure.Edges[i], edge)
		}
	}
}

// Files outside the V1 language set are not modules: a Markdown file says nothing
// about construction.
func TestStructureOfIgnoresNonSourceFiles(t *testing.T) {
	structure, _ := StructureOf([]File{{Path: "docs/notes.md", Body: []byte("# hi\n")}})
	if len(structure.Modules) != 0 {
		t.Fatalf("a Markdown file became a module: %+v", structure.Modules)
	}
}

// A file with a syntax error still belongs to its module, but contributes no
// edges: a half-read file is unknown, and unknown is not an invented dependency.
func TestStructureOfDoesNotInventEdgesFromBrokenFiles(t *testing.T) {
	structure, _ := StructureOf([]File{{Path: "internal/broken/broken.go", Body: []byte("package broken\nimport (\n")}})
	broken := module(t, structure, "internal/broken")
	if len(broken.DependsOn) != 0 || len(structure.Edges) != 0 {
		t.Fatalf("a broken file invented edges: %+v", structure.Edges)
	}
}

func module(t *testing.T, structure Structure, dir string) Module {
	t.Helper()
	for _, m := range structure.Modules {
		if m.Dir == dir {
			return m
		}
	}
	t.Fatalf("no module %q in %+v", dir, structure.Modules)
	return Module{}
}
