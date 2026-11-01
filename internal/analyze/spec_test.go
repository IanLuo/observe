package analyze

import (
	"testing"

	"observe/internal/model"
)

// AC7: a repository with no usable locked spec keeps browsing available and
// marks goal alignment unavailable.
func TestBaselineMissingWhenNoLockedSpec(t *testing.T) {
	got := Baseline([]Doc{
		{Path: "README.md", Family: "design"},
		{Path: ".agents/specs/architecture.md", Family: "specs", Type: "architecture"},
	})
	if got.State != model.BaselineMissing {
		t.Fatalf("state = %q, want %q", got.State, model.BaselineMissing)
	}
	if len(got.SpecPaths) != 0 {
		t.Fatalf("a missing baseline named specs: %v", got.SpecPaths)
	}
	if len(got.Diagnostics) == 0 {
		t.Fatal("a missing baseline reported no diagnostic")
	}
}

func TestBaselineAvailableWithOneLockedSpec(t *testing.T) {
	got := Baseline([]Doc{
		{Path: ".agents/specs/spec.md", Family: "specs", Type: "spec"},
		{Path: ".agents/specs/system-design.md", Family: "specs", Type: "system-design"},
	})
	if got.State != model.BaselineAvailable {
		t.Fatalf("state = %q, want %q", got.State, model.BaselineAvailable)
	}
	if len(got.SpecPaths) != 1 || got.SpecPaths[0] != ".agents/specs/spec.md" {
		t.Fatalf("spec paths = %v", got.SpecPaths)
	}
}

// AC8: two locked specs with competing goals are reported unresolved, and
// neither is silently preferred.
func TestBaselineConflictingWithTwoLockedSpecs(t *testing.T) {
	got := Baseline([]Doc{
		{Path: "a/spec.md", Family: "specs", Type: "spec"},
		{Path: "b/spec.md", Family: "specs", Type: "spec"},
	})
	if got.State != model.BaselineConflicting {
		t.Fatalf("state = %q, want %q", got.State, model.BaselineConflicting)
	}
	if len(got.SpecPaths) != 2 {
		t.Fatalf("both specs must be named, got %v", got.SpecPaths)
	}
	if len(got.Diagnostics) == 0 {
		t.Fatal("a conflicting baseline reported no diagnostic")
	}
}

func TestRequirementsExtractsTopLevelItemsOnly(t *testing.T) {
	body := []byte(`<!-- specs:locked:abc1234 2026-01-01 type=spec -->

## Link contract
- upstream: ignored
- referrers: ignored

## Non-negotiable outcome
- the product shows goal alignment
- uncertainty stays explicit

### Detail
- a nested heading still counts as a requirement

## Scope
  - an indented bullet is detail, not a requirement
- last one
`)
	goals := Requirements(".agents/specs/spec.md", body)

	want := []string{
		"the product shows goal alignment",
		"uncertainty stays explicit",
		"a nested heading still counts as a requirement",
		"last one",
	}
	if len(goals) != len(want) {
		t.Fatalf("got %d requirements, want %d: %+v", len(goals), len(want), goals)
	}
	for i, g := range goals {
		if g.Label != want[i] {
			t.Errorf("requirement %d = %q, want %q", i, g.Label, want[i])
		}
		if g.SpecPath != ".agents/specs/spec.md" {
			t.Errorf("requirement %d spec path = %q", i, g.SpecPath)
		}
	}
	if got := goals[0].HeadingChain; len(got) != 1 || got[0] != "Non-negotiable outcome" {
		t.Errorf("heading chain = %v", got)
	}
	if got := goals[2].HeadingChain; len(got) != 2 || got[1] != "Detail" {
		t.Errorf("nested heading chain = %v", got)
	}
}

func TestRequirementsSkipsFencedCode(t *testing.T) {
	body := []byte("## Scope\n\n```\n- not a requirement\n```\n\n- a requirement\n")
	goals := Requirements("spec.md", body)
	if len(goals) != 1 || goals[0].Label != "a requirement" {
		t.Fatalf("goals = %+v", goals)
	}
}

// The text hash identifies the goal, so restyling the text must not read as a
// different requirement (system-design, Goal locator).
func TestGoalTextHashIgnoresEmphasis(t *testing.T) {
	plain := Requirements("spec.md", []byte("## G\n- a goal with words\n"))
	bold := Requirements("spec.md", []byte("## G\n- **a goal with words**\n"))

	if len(plain) != 1 || len(bold) != 1 {
		t.Fatalf("plain=%+v bold=%+v", plain, bold)
	}
	if plain[0].TextHash != bold[0].TextHash {
		t.Fatalf("emphasis changed the goal identity: %q vs %q", plain[0].TextHash, bold[0].TextHash)
	}
}
