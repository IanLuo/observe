package analyze

import (
	"strings"
	"testing"

	"observe/internal/model"
)

func goal(label string) model.GoalLocator {
	return model.GoalLocator{SpecPath: ".agents/specs/spec.md", Label: label}
}

func decl(name, path string, line int) Declaration {
	return Declaration{Name: name, Path: path, LineStart: line, LineEnd: line}
}

// AC1: a requirement naming a symbol is shown with an inferred code link.
func TestAssessLinksGoalToSymbol(t *testing.T) {
	got := Assess(
		[]model.GoalLocator{goal("the reader exposes `Open` for a repository")},
		[]Declaration{decl("Open", "internal/repo/repo.go", 42)},
	)
	if len(got) != 1 {
		t.Fatalf("assessments = %+v", got)
	}
	a := got[0]
	if len(a.Evidence) != 1 {
		t.Fatalf("evidence = %+v", a.Evidence)
	}
	if !a.Inferred {
		t.Error("a text-matching link must be marked inferred")
	}
	if a.Evidence[0].Symbol != "Open" || a.Evidence[0].LineStart != 42 {
		t.Errorf("evidence = %+v", a.Evidence[0])
	}
	if a.Relation != model.RelationUnknown {
		t.Errorf("relation = %q; with no selected diff it must stay unknown, not claim support", a.Relation)
	}
}

// AC1: a requirement with no matching code carries the explicit state, not silence.
func TestAssessReportsNoCodeLinkFound(t *testing.T) {
	got := Assess([]model.GoalLocator{goal("the product shows a map")}, nil)
	if len(got) != 1 {
		t.Fatalf("assessments = %+v", got)
	}
	a := got[0]
	if a.Relation != model.RelationUnmapped {
		t.Errorf("relation = %q, want %q", a.Relation, model.RelationUnmapped)
	}
	if len(a.Evidence) != 0 {
		t.Errorf("evidence = %+v, want none", a.Evidence)
	}
	if !hasDiagnostic(a, "no code link found") {
		t.Errorf("no 'no code link found' diagnostic: %+v", a.Diagnostics)
	}
}

// AC3: a test is evidence that a test exists, never that the goal is verified.
func TestAssessKeepsVerificationUnknownEvenWhenATestMatches(t *testing.T) {
	test := decl("TestOpen", "internal/repo/repo_test.go", 10)
	test.IsTest = true

	got := Assess([]model.GoalLocator{goal("opening is covered by `TestOpen`")}, []Declaration{test})
	a := got[0]

	if len(a.VerificationEvidence) != 1 {
		t.Fatalf("verification evidence = %+v", a.VerificationEvidence)
	}
	if a.Verification != model.VerificationUnknown {
		t.Errorf("verification = %q, want %q: a discovered test is not a passing result", a.Verification, model.VerificationUnknown)
	}
	if len(a.Evidence) != 0 {
		t.Errorf("a test must not count as implementation evidence: %+v", a.Evidence)
	}
	if a.Relation != model.RelationUnmapped {
		t.Errorf("relation = %q, want %q", a.Relation, model.RelationUnmapped)
	}
}

// A directory named in the requirement links to the declarations inside it, with
// lower confidence than a symbol match.
func TestAssessLinksGoalToDirectory(t *testing.T) {
	got := Assess(
		[]model.GoalLocator{goal("the cache lives in `internal/store`")},
		[]Declaration{decl("Cache", "internal/store/cache.go", 7), decl("Open", "internal/repo/repo.go", 42)},
	)
	a := got[0]
	if len(a.Evidence) != 1 || a.Evidence[0].Symbol != "Cache" {
		t.Fatalf("evidence = %+v", a.Evidence)
	}
	if a.Evidence[0].Confidence >= 0.9 {
		t.Errorf("a directory match scored %v, as high as a symbol match", a.Evidence[0].Confidence)
	}
}

// A word of the requirement that happens to name a declaration is a link; a word
// that names nothing is not.
func TestAssessMatchesBareSymbolNames(t *testing.T) {
	got := Assess(
		[]model.GoalLocator{goal("Feed keeps the last snapshot")},
		[]Declaration{decl("Feed", "internal/serve/feed.go", 3)},
	)
	if len(got[0].Evidence) != 1 {
		t.Fatalf("evidence = %+v", got[0].Evidence)
	}
}

func TestAssessBoundsEvidenceAndSaysSo(t *testing.T) {
	var decls []Declaration
	for i := 0; i < maxEvidencePerGoal+5; i++ {
		decls = append(decls, decl("Alpha", "internal/x/x.go", i+1))
	}
	got := Assess([]model.GoalLocator{goal("`internal/x` holds it")}, decls)
	a := got[0]
	if len(a.Evidence) != maxEvidencePerGoal {
		t.Fatalf("evidence = %d, want %d", len(a.Evidence), maxEvidencePerGoal)
	}
	if !hasDiagnostic(a, "showing") {
		t.Errorf("truncation was silent: %+v", a.Diagnostics)
	}
}

func hasDiagnostic(a model.GoalAssessment, contains string) bool {
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, contains) {
			return true
		}
	}
	return false
}
