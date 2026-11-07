package analyze

import (
	"testing"
)

// modules bodies: a imports b and c; b imports nothing; c imports nothing.
func coupledFixture() []File {
	return []File{
		{Path: "internal/a/a.go", Body: []byte("package a\n\nimport (\n\t_ \"x/internal/b\"\n\t_ \"x/internal/c\"\n)\n")},
		{Path: "internal/b/b.go", Body: []byte("package b\n\nfunc B() {}\n")},
		{Path: "internal/c/c.go", Body: []byte("package c\n\nfunc C() {}\n")},
	}
}

// AC16: coupling is counted from the module edges, and instability is fan-out over
// total coupling.
func TestCouplingCountsInboundAndOutbound(t *testing.T) {
	structure, diags := StructureOf(coupledFixture())
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	byDir := map[string]Module{}
	for _, m := range structure.Modules {
		byDir[m.Dir] = m
	}

	a := byDir["internal/a"]
	if a.FanOut != 2 || a.FanIn != 0 || a.Instability != 1 {
		t.Errorf("a = in %d out %d instability %v, want 0/2/1", a.FanIn, a.FanOut, a.Instability)
	}
	b := byDir["internal/b"]
	if b.FanIn != 1 || b.FanOut != 0 || b.Instability != 0 {
		t.Errorf("b = in %d out %d instability %v, want 1/0/0", b.FanIn, b.FanOut, b.Instability)
	}
}

// AC16: a cycle is reported with its members, so the reader can open them.
func TestCyclesAreReportedWithTheirMembers(t *testing.T) {
	files := []File{
		{Path: "internal/a/a.go", Body: []byte("package a\n\nimport _ \"x/internal/b\"\n")},
		{Path: "internal/b/b.go", Body: []byte("package b\n\nimport _ \"x/internal/c\"\n")},
		{Path: "internal/c/c.go", Body: []byte("package c\n\nimport _ \"x/internal/a\"\n")},
		{Path: "internal/d/d.go", Body: []byte("package d\n\nfunc D() {}\n")},
	}
	structure, diags := StructureOf(files)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	if len(structure.Cycles) != 1 {
		t.Fatalf("cycles = %v, want one", structure.Cycles)
	}
	got := structure.Cycles[0]
	if len(got) != 3 || got[0] != "internal/a" || got[1] != "internal/b" || got[2] != "internal/c" {
		t.Errorf("cycle = %v, want a, b, c", got)
	}
}

// An acyclic project says so: the analysis looked, and the answer is none.
func TestNoCyclesIsAnEmptyAnswer(t *testing.T) {
	structure, _ := StructureOf(coupledFixture())
	if len(structure.Cycles) != 0 {
		t.Fatalf("cycles = %v, want none", structure.Cycles)
	}
}

// A module that imports itself through two files is not a cycle: the module graph
// hides intra-module imports.
func TestSelfImportsAreNotCycles(t *testing.T) {
	files := []File{
		{Path: "internal/a/one.go", Body: []byte("package a\n\nimport _ \"x/internal/a\"\n")},
	}
	structure, _ := StructureOf(files)
	if len(structure.Cycles) != 0 {
		t.Fatalf("cycles = %v, want none", structure.Cycles)
	}
}
