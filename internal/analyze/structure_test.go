package analyze

import (
	"fmt"
	"strings"
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
	if want := []string{"internal/a/a.go", "internal/a/a_test.go"}; !equal(a.Files, want) {
		t.Errorf("internal/a files = %v, want %v", a.Files, want)
	}
	// Every declaration in the module is carried with its line, test or not, because
	// the drill-down lists symbols and their locations.
	if len(a.Decls) != 2 {
		t.Fatalf("internal/a decls = %d, want 2 (%+v)", len(a.Decls), a.Decls)
	}
	if a.Decls[0].Name != "A" || a.Decls[0].IsTest || a.Decls[0].LineStart != 9 {
		t.Errorf("first decl = %+v, want A at line 9, not a test", a.Decls[0])
	}
	if a.Decls[1].Name != "TestA" || !a.Decls[1].IsTest {
		t.Errorf("second decl = %+v, want the test TestA", a.Decls[1])
	}

	// An import that names no module here is a real dependency, but not an edge:
	// calling it external keeps the diagram honest instead of guessing a target.
	// "testing" is absent because test files are counted, not wired.
	if len(a.Imports) != 1 || a.Imports[0] != "net/http" {
		t.Errorf("internal/a external imports = %v, want [net/http]", a.Imports)
	}

	// The drill-down needs the imports that stay inside a module, which the module
	// graph hides: web/main.ts imports web/render.ts.
	var fileEdges []string
	for _, e := range structure.FileEdges {
		if e.Kind == "import" {
			fileEdges = append(fileEdges, e.From+" -> "+e.To)
		}
	}
	if !equal(fileEdges, []string{"web/main.ts -> web/render.ts"}) {
		t.Errorf("file edges = %v, want web/main.ts -> web/render.ts", fileEdges)
	}

	// A relative TypeScript import names a file inside its own module, so it is
	// neither a module edge nor an external dependency: the UI is one module.
	web := module(t, structure, "web")
	if len(web.DependsOn) != 0 || len(web.Imports) != 0 {
		t.Errorf("web depends on %v / imports %v, want neither", web.DependsOn, web.Imports)
	}
	if len(web.Files) != 2 || len(web.Decls) != 2 {
		t.Errorf("web = %d files, %d decls, want 2/2", len(web.Files), len(web.Decls))
	}

	wantEdges := []ModuleEdge{
		{From: "cmd/tool", To: "internal/a", Import: "observe/internal/a", Path: "cmd/tool/main.go", Line: 6},
		{From: "internal/a", To: "internal/b", Import: "observe/internal/b", Path: "internal/a/a.go", Line: 6},
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

// A module with many declarations per file is cut, and says so: a truncated list
// must never look complete.
func TestStructureOfCapsDeclarationsPerFile(t *testing.T) {
	var body strings.Builder
	body.WriteString("package big\n")
	for i := 0; i < maxModuleDeclsPerFile+5; i++ {
		fmt.Fprintf(&body, "\nfunc F%d() {}\n", i)
	}
	structure, _ := StructureOf([]File{{Path: "internal/big/big.go", Body: []byte(body.String())}})
	big := module(t, structure, "internal/big")
	if len(big.Decls) != maxModuleDeclsPerFile {
		t.Fatalf("decls = %d, want the cap %d", len(big.Decls), maxModuleDeclsPerFile)
	}
	if !big.Truncated {
		t.Error("a capped module did not report the truncation")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

// Two files in one Go package never import each other, so the only structure
// between them is symbol use. That relation is what the drill-down inside a module
// draws, and it stays inside the module.
func TestStructureOfLinksFilesBySymbolReference(t *testing.T) {
	files := []File{
		{Path: "internal/a/types.go", Body: []byte("package a\n\ntype Thing struct{}\n\nfunc (Thing) Use() {}\n")},
		{Path: "internal/a/user.go", Body: []byte("package a\n\nfunc User() Thing { return Thing{} }\n")},
		{Path: "internal/b/other.go", Body: []byte("package b\n\nfunc Other() { _ = User }\n")},
	}
	structure, diags := StructureOf(files)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	var refs []string
	for _, edge := range structure.FileEdges {
		if edge.Kind == "reference" {
			refs = append(refs, edge.From+" -> "+edge.To+" ("+edge.Symbol+")")
		}
	}
	if !equal(refs, []string{"internal/a/user.go -> internal/a/types.go (Thing)"}) {
		t.Fatalf("reference edges = %v, want user.go -> types.go (Thing)", refs)
	}

	// internal/b mentions User, which is declared in another module: that is not a
	// file relation, and inventing one would cross a package boundary.
	for _, edge := range structure.FileEdges {
		if edge.From == "internal/b/other.go" {
			t.Fatalf("a cross-module reference was drawn: %+v", edge)
		}
	}
}

// A TypeScript workspace imports its siblings by package name, so a monorepo's real
// edges only appear when package.json names are known.
func TestStructureOfResolvesWorkspacePackages(t *testing.T) {
	files := []File{
		{Path: "packages/domain/package.json", Body: []byte(`{"name": "@app/domain"}`)},
		{Path: "packages/domain/src/index.ts", Body: []byte("export type Id = string;\n")},
		{Path: "packages/db/package.json", Body: []byte(`{"name": "@app/db"}`)},
		{Path: "packages/db/src/store.ts", Body: []byte("import { Id } from \"@app/domain\";\n\nexport function get(id: Id): void {}\n")},
	}
	structure, diags := StructureOf(files)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	db := module(t, structure, "packages/db/src")
	if len(db.DependsOn) != 1 || db.DependsOn[0] != "packages/domain/src" {
		t.Fatalf("packages/db/src depends on %v, want packages/domain/src", db.DependsOn)
	}
	if len(db.Imports) != 0 {
		t.Errorf("a workspace package was left external: %v", db.Imports)
	}
}
