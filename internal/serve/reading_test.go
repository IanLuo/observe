package serve

import (
	"strings"
	"testing"

	"observe/internal/analyze"
	"observe/internal/model"
)

// AC15: the goal says what its verdicts do and do not prove.
func TestReadingStatesWhatTheVerdictsDoNotProve(t *testing.T) {
	presence := func(status model.CheckStatus) Check {
		return Check{ID: "x", Measures: string(model.MeasuresPresence), Status: string(status)}
	}
	cases := []struct {
		name   string
		checks []Check
		want   []string
		deny   []string
	}{
		{
			name:   "no check declared",
			checks: nil,
			want:   []string{"No check defined", "claim"},
		},
		{
			name:   "presence only",
			checks: []Check{presence(model.CheckPass)},
			want:   []string{"The check holds", "a name, not behaviour", "no test names it"},
			deny:   []string{"behaviour holds"},
		},
		{
			name:   "one violated",
			checks: []Check{presence(model.CheckPass), presence(model.CheckViolated)},
			want:   []string{"1 of 2 checks violated", "names, not behaviour"},
		},
		{
			name:   "could not decide",
			checks: []Check{presence(model.CheckUnknown)},
			want:   []string{"1 of 1 checks could not be decided"},
		},
		{
			name: "test covered",
			checks: []Check{{
				ID: "t", Measures: string(model.MeasuresTest), Status: string(model.CheckPass),
				Verification: string(model.VerificationVerified),
			}},
			want: []string{"coverage and not a passing result"},
			deny: []string{"names, not behaviour"},
		},
		{
			name:   "dependency",
			checks: []Check{{ID: "d", Measures: string(model.MeasuresDependency), Status: string(model.CheckPass)}},
			want:   []string{"The check holds"},
			deny:   []string{"names, not behaviour"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reading(c.checks)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("reading = %q, want %q", got, want)
				}
			}
			for _, deny := range c.deny {
				if strings.Contains(got, deny) {
					t.Errorf("reading = %q, must not say %q", got, deny)
				}
			}
		})
	}
}

// The finding is the verdict said in the words of what was looked at.
func TestFindingUsesTheWordsOfTheMeasure(t *testing.T) {
	cases := []struct {
		assertion string
		status    model.CheckStatus
		want      string
	}{
		{"exists", model.CheckPass, "present"},
		{"exists", model.CheckViolated, "not present"},
		{"absent", model.CheckPass, "not present"},
		{"absent", model.CheckViolated, "present"},
		{"imports internal/repo", model.CheckPass, "wired"},
		{"imports internal/repo", model.CheckViolated, "not wired"},
		{"not-imports internal/serve", model.CheckPass, "no such import"},
		{"not-imports internal/serve", model.CheckViolated, "imports anyway"},
		{"has-test", model.CheckPass, "tests found"},
		{"has-test", model.CheckViolated, "no tests found"},
		{"exists", model.CheckUnknown, "not decided"},
	}
	for _, c := range cases {
		if got := finding(model.Check{Assertion: c.assertion, Status: c.status}); got != c.want {
			t.Errorf("%q %q = %q, want %q", c.assertion, c.status, got, c.want)
		}
	}
}

// AC17: a hotspot is a declaration that is hard to read and often changed. Either
// fact alone is not enough.
func TestHotspotsNeedComplexityAndChurn(t *testing.T) {
	structure := analyze.Structure{Modules: []analyze.Module{{
		Dir: "internal/a",
		Decls: []analyze.Declaration{
			{Name: "HardAndHot", Path: "internal/a/a.go", LineStart: 10, Complexity: 12},
			{Name: "HardAndCold", Path: "internal/a/b.go", LineStart: 20, Complexity: 30},
			{Name: "SimpleAndHot", Path: "internal/a/c.go", LineStart: 30, Complexity: 1},
		},
	}}}
	churn := map[string]int{"internal/a/a.go": 5, "internal/a/b.go": 0, "internal/a/c.go": 9}

	got := hotspots(structure, churn)
	if len(got) != 1 {
		t.Fatalf("hotspots = %+v, want only the one that is both", got)
	}
	if got[0].Symbol != "HardAndHot" || got[0].Complexity != 12 || got[0].Churn != 5 {
		t.Errorf("hotspot = %+v", got[0])
	}
}

// AC16: a module in a cycle says so, so the map can point at it.
func TestModulesCarryTheirCycle(t *testing.T) {
	structure := analyze.Structure{
		Modules: []analyze.Module{{Dir: "internal/a"}, {Dir: "internal/b"}, {Dir: "internal/c"}},
		Cycles:  [][]string{{"internal/a", "internal/b"}},
	}
	got := structureOf(structure, nil)
	flags := map[string]bool{}
	for _, m := range got.Modules {
		flags[m.Dir] = m.InCycle
	}
	if !flags["internal/a"] || !flags["internal/b"] || flags["internal/c"] {
		t.Errorf("in-cycle flags = %v", flags)
	}
}
