package analyze

import "testing"

func TestDeclarationsFindsGoFunctionsMethodsAndTypes(t *testing.T) {
	src := []byte(`package demo

type Snapshot struct{}

func Build() {}

func (s Snapshot) Hash() {}
`)
	decls, diags := Declarations([]File{{Path: "internal/demo/demo.go", Body: src}})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	want := map[string]int{"Snapshot": 3, "Build": 5, "Hash": 7}
	got := map[string]int{}
	for _, d := range decls {
		got[d.Name] = d.LineStart
		if d.Path != "internal/demo/demo.go" {
			t.Errorf("%s has path %q", d.Name, d.Path)
		}
	}
	for name, line := range want {
		if got[name] != line {
			t.Errorf("%s starts at line %d, want %d", name, got[name], line)
		}
	}
}

func TestDeclarationsFindsTypeScriptSymbols(t *testing.T) {
	src := []byte(`export interface Goal { name: string }
export type Relation = "unknown";
export class Store { save() {} }
export function assess(): void {}
`)
	decls, diags := Declarations([]File{{Path: "web/assess.ts", Body: src}})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	got := map[string]bool{}
	for _, d := range decls {
		got[d.Name] = true
	}
	for _, name := range []string{"Goal", "Relation", "Store", "assess", "save"} {
		if !got[name] {
			t.Errorf("did not find %s; found %v", name, got)
		}
	}
}

// A file outside the V1 language set must be reported, never skipped silently.
func TestDeclarationsReportsUnsupportedLanguage(t *testing.T) {
	_, diags := Declarations([]File{{Path: "web/app.js", Body: []byte("function x() {}")}})
	if len(diags) != 1 || diags[0].Path != "web/app.js" {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDeclarationsMarksTestSymbols(t *testing.T) {
	decls, _ := Declarations([]File{
		{Path: "internal/demo/demo_test.go", Body: []byte("package demo\n\nfunc TestBuild() {}\n")},
		{Path: "web/assess.spec.ts", Body: []byte("function helper() {}\n")},
	})
	if len(decls) != 2 {
		t.Fatalf("declarations = %+v", decls)
	}
	for _, d := range decls {
		if !d.IsTest {
			t.Errorf("%s in %s was not marked as a test", d.Name, d.Path)
		}
	}
}

func TestIsSourcePathCoversOnlyTheV1Languages(t *testing.T) {
	for path, want := range map[string]bool{
		"a.go": true, "a.ts": true, "a.tsx": true,
		"a.js": false, "a.jsx": false, "a.py": false, "a.md": false,
	} {
		if got := IsSourcePath(path); got != want {
			t.Errorf("IsSourcePath(%q) = %v, want %v", path, got, want)
		}
	}
}
