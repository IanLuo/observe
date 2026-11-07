package analyze

import "testing"

// AC17: complexity is one plus the decision points in the declaration's own syntax,
// so a reader can re-derive the number by looking at the code.
func TestComplexityCountsDecisionPoints(t *testing.T) {
	files := []File{
		{Path: "internal/a/a.go", Body: []byte(`package a

func Straight() {}

func Branches(x int, s string) int {
	if x > 1 && s != "" {
		return 1
	}
	for i := 0; i < x; i++ {
		if i%2 == 0 || i == 3 {
			return i
		}
	}
	return 0
}
`)},
		{Path: "web/main.ts", Body: []byte(`export function straight(): number { return 1; }

export function branches(x: number, s: string): number {
  if (x > 1 || s === "") {
    return 1;
  }
  while (x > 0) {
    x--;
  }
  return x > 2 ? 2 : 0;
}
`)},
	}

	decls, diags := Declarations(files)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	want := map[string]int{
		"Straight": 1,
		"Branches": 1 + 2 + 1 + 2, // if + (&&), for, if + (||): two "if"s, one "for"
		"straight": 1,
		"branches": 1 + 2 + 1 + 1, // if + (||), while, ternary
	}
	for _, d := range decls {
		if expected, ok := want[d.Name]; ok && d.Complexity != expected {
			t.Errorf("%s complexity = %d, want %d", d.Name, d.Complexity, expected)
		}
	}
}

// A class is not one path through the code, and a handler registered inside a
// factory keeps its own branches: counting them against the factory inflated a
// 400-line server factory to complexity 108.
func TestComplexityStaysInsideTheFunction(t *testing.T) {
	files := []File{
		{Path: "web/server.ts", Body: []byte(`class Store {
  load(x: number): number {
    if (x > 0) {
      return 1;
    }
    return 0;
  }
}

export function factory(): void {
  const handler = (x: number) => {
    if (x) { return; }
    if (!x) { return; }
  };
  void handler;
}
`)},
	}

	decls, _ := Declarations(files)
	got := map[string]int{}
	for _, d := range decls {
		got[d.Name] = d.Complexity
	}
	if got["Store"] != 0 {
		t.Errorf("class complexity = %d, want 0", got["Store"])
	}
	if got["load"] != 2 {
		t.Errorf("method complexity = %d, want 2", got["load"])
	}
	if got["factory"] != 1 {
		t.Errorf("factory complexity = %d, want 1: the nested handler owns its branches", got["factory"])
	}
}
