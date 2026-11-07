package serve

import (
	"strings"
	"testing"

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
