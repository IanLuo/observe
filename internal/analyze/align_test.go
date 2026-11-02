package analyze

import (
	"reflect"
	"testing"

	"observe/internal/model"
)

const repoFile = "internal/repo/repo.go"

// AC2: a diff that adds lines inside code a goal names is classified
// appears-to-support, and the classification carries the declaration that
// justifies it.
func TestAlignClassifiesSupport(t *testing.T) {
	change := Change{
		Path:   repoFile,
		Status: " M",
		Diff:   "@@ -3,1 +3,3 @@\n-func Open() {}\n+func Open() {\n+\treturn\n+}\n",
		Old:    []byte("package repo\n\nfunc Open() {}\n"),
		New:    []byte("package repo\n\nfunc Open() {\n\treturn\n}\n"),
	}
	decls := []Declaration{{Name: "Open", Path: repoFile, LineStart: 3, LineEnd: 5}}

	got, diags := Align(
		Assess([]model.GoalLocator{goal("the reader exposes `Open`")}, decls),
		decls,
		[]Change{change},
	)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	a := got[0]
	if a.Relation != model.RelationAppearsToSupport {
		t.Fatalf("relation = %q, want %q", a.Relation, model.RelationAppearsToSupport)
	}
	if !a.Inferred {
		t.Error("a diff-matched classification is still inferred, not proof")
	}
	if len(a.Evidence) == 0 || a.Evidence[0].Symbol != "Open" {
		t.Fatalf("evidence = %+v", a.Evidence)
	}
	if a.Verification != model.VerificationUnknown {
		t.Errorf("verification = %q: a diff never verifies anything", a.Verification)
	}
}

// AC2: a diff that removes the code a goal names is a conflict, not support.
func TestAlignClassifiesConflictWhenTheDiffRemovesTheNamedCode(t *testing.T) {
	change := Change{
		Path:   repoFile,
		Status: " M",
		Diff:   "@@ -3,2 +3,1 @@\n-func Open() {}\n-func Close() {}\n+// Open was removed.\n",
		Old:    []byte("package repo\n\nfunc Open() {}\nfunc Close() {}\n"),
		New:    []byte("package repo\n\n// Open was removed.\n"),
	}
	// The working tree no longer declares Open, so the goal has no link there;
	// the conflict must still be detected from the pre-diff content.
	got, _ := Align(
		Assess([]model.GoalLocator{goal("the reader exposes `Open`")}, nil),
		nil,
		[]Change{change},
	)
	a := got[0]
	if a.Relation != model.RelationConflict {
		t.Fatalf("relation = %q, want %q", a.Relation, model.RelationConflict)
	}
	if len(a.Evidence) == 0 {
		t.Fatal("a conflict must carry the evidence that justifies it")
	}
	if a.Evidence[0].Symbol != "Open" || a.Evidence[0].Path != repoFile {
		t.Errorf("evidence = %+v", a.Evidence)
	}
}

// AC2: a requirement that names the removed code in prose, not in backticks, is
// still a conflict. The name only exists in the pre-diff tree, so the term set
// has to span both.
func TestAlignClassifiesConflictForABareWordRequirement(t *testing.T) {
	change := Change{
		Path:   repoFile,
		Status: " M",
		Diff:   "@@ -3,1 +3,1 @@\n-func Open() {}\n+// gone\n",
		Old:    []byte("package repo\n\nfunc Open() {}\n"),
		New:    []byte("package repo\n\n// gone\n"),
	}
	got, _ := Align(
		Assess([]model.GoalLocator{goal("the reader exposes Open for a repository")}, nil),
		nil,
		[]Change{change},
	)
	if got[0].Relation != model.RelationConflict {
		t.Fatalf("relation = %q, want %q", got[0].Relation, model.RelationConflict)
	}
}

// Moved code keeps its name, so it is not a conflict.
func TestAlignDoesNotCallAMoveAConflict(t *testing.T) {
	change := Change{
		Path:   "internal/repo/open.go",
		Status: "R ",
		Diff:   "@@ -1,3 +1,3 @@\n package repo\n \n func Open() {}\n",
		Old:    []byte("package repo\n\nfunc Open() {}\n"),
		New:    []byte("package repo\n\nfunc Open() {}\n"),
	}
	decls := []Declaration{{Name: "Open", Path: "internal/repo/open.go", LineStart: 3, LineEnd: 3}}
	got, _ := Align(
		Assess([]model.GoalLocator{goal("the reader exposes `Open`")}, decls),
		decls,
		[]Change{change},
	)
	if got[0].Relation == model.RelationConflict {
		t.Fatalf("a rename that keeps the declaration was reported as a conflict")
	}
}

// AC2: a new untracked file is part of the diff, and the goal it implements is
// supported even though git has no diff text for it.
func TestAlignClassifiesSupportForANewFile(t *testing.T) {
	change := Change{
		Path:   repoFile,
		Status: "??",
		New:    []byte("package repo\n\nfunc Open() error { return nil }\n"),
	}
	// The untracked file is not in the pre-analysis declaration set, so the link
	// exists only because the diff supplied it.
	got, _ := Align(
		Assess([]model.GoalLocator{goal("the reader exposes `Open`")}, nil),
		nil,
		[]Change{change},
	)
	a := got[0]
	if a.Relation != model.RelationAppearsToSupport {
		t.Fatalf("relation = %q, want %q", a.Relation, model.RelationAppearsToSupport)
	}
	if len(a.Evidence) != 1 || a.Evidence[0].Symbol != "Open" {
		t.Fatalf("evidence = %+v", a.Evidence)
	}
	if hasDiagnostic(a, "no code link found") {
		t.Errorf("stale pre-diff diagnostic survived the classification: %+v", a.Diagnostics)
	}
	if a.Verification != model.VerificationUnknown {
		t.Errorf("verification = %q", a.Verification)
	}
}

// AC2: a change that does not touch a goal's code is never labeled aligned.
func TestAlignNeverLabelsAnUntouchedGoalAligned(t *testing.T) {
	change := Change{
		Path:   "internal/store/cache.go",
		Status: " M",
		Diff:   "@@ -1,1 +1,2 @@\n package store\n+// Cache is next.\n",
		Old:    []byte("package store\n"),
		New:    []byte("package store\n// Cache is next.\n"),
	}
	decls := []Declaration{
		{Name: "Open", Path: repoFile, LineStart: 3, LineEnd: 3},
		{Name: "Cache", Path: "internal/store/cache.go", LineStart: 2, LineEnd: 2},
	}
	got, _ := Align(
		Assess([]model.GoalLocator{goal("the reader exposes `Open`")}, decls),
		decls,
		[]Change{change},
	)
	a := got[0]
	if a.Relation == model.RelationAppearsToSupport {
		t.Fatalf("relation = %q: the diff never touches Open", a.Relation)
	}
	if a.Relation != model.RelationUnmapped {
		t.Fatalf("relation = %q, want %q", a.Relation, model.RelationUnmapped)
	}
	if !hasDiagnostic(a, "does not touch") {
		t.Errorf("no explanation for the unmapped classification: %+v", a.Diagnostics)
	}
}

// AC3: touching a test file is not verification evidence.
func TestAlignKeepsVerificationUnknownForATestFileChange(t *testing.T) {
	testFile := "internal/repo/repo_test.go"
	change := Change{
		Path:   testFile,
		Status: " M",
		Diff:   "@@ -1,1 +1,2 @@\n package repo\n+func TestOpen(t *testing.T) {}\n",
		Old:    []byte("package repo\n"),
		New:    []byte("package repo\nfunc TestOpen(t *testing.T) {}\n"),
	}
	decls := []Declaration{{Name: "TestOpen", Path: testFile, LineStart: 2, LineEnd: 2, IsTest: true}}
	got, _ := Align(
		Assess([]model.GoalLocator{goal("opening is covered by `TestOpen`")}, decls),
		decls,
		[]Change{change},
	)
	a := got[0]
	if a.Verification != model.VerificationUnknown {
		t.Fatalf("verification = %q: a changed test is not a passing test", a.Verification)
	}
	if len(a.VerificationEvidence) == 0 {
		t.Error("the test declaration should still be listed as verification evidence")
	}
}

// With no selected diff the relation stays what Assess produced: a baseline-only
// view makes no claim about a change.
func TestAlignWithoutASelectedDiffLeavesRelationsAlone(t *testing.T) {
	decls := []Declaration{{Name: "Open", Path: repoFile, LineStart: 3, LineEnd: 3}}
	before := Assess([]model.GoalLocator{goal("the reader exposes `Open`"), goal("the map shows goals")}, decls)
	after, diags := Align(before, decls, nil)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	for i := range before {
		if after[i].Relation != before[i].Relation {
			t.Errorf("goal %d: relation %q became %q with no diff", i, before[i].Relation, after[i].Relation)
		}
	}
}

// Lines gives the added and removed lines with their own-side line numbers, which
// is what attributes a change to a declaration.
func TestLinesReportsAddedAndRemovedLines(t *testing.T) {
	diff := "@@ -10,3 +10,4 @@ context\n context\n-removed\n kept\n+added\n"
	got := Lines(diff)
	want := []DiffLine{
		{Line: 11, Text: "removed", Added: false},
		{Line: 12, Text: "added", Added: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

const promptSpec = `<!-- specs:locked:abc1234 2026-01-01 type=spec -->

# Spec
## Verification
- Verification stays ` + "`unknown`" + ` unless explicit evidence exists.
`

// AC5: a task prompt that contradicts a locked requirement is shown unresolved,
// and the locked spec remains the baseline.
func TestPromptConflictIsShownAndTheSpecStaysTheBaseline(t *testing.T) {
	docs := []Doc{{Path: ".agents/specs/spec.md", Family: "specs", Type: "spec"}}
	baseline := Baseline(docs)
	goals := Requirements(baseline.SpecPaths[0], []byte(promptSpec))
	if len(goals) != 1 {
		t.Fatalf("fixture goals = %d, want 1", len(goals))
	}

	prompt := "Do not keep verification `unknown`; treat code presence as enough."
	conflicts := PromptConflicts(goals, prompt)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one", conflicts)
	}
	if conflicts[0].Locator.SpecPath != baseline.SpecPaths[0] {
		t.Errorf("conflict points at %q, want the locked spec %q", conflicts[0].Locator.SpecPath, baseline.SpecPaths[0])
	}
	if conflicts[0].Prompt == "" {
		t.Error("a conflict must quote the prompt text that contradicts the goal")
	}

	// The prompt is context only: the baseline and the goal set are unchanged.
	if after := Baseline(docs); after.State != baseline.State || !reflect.DeepEqual(after.SpecPaths, baseline.SpecPaths) {
		t.Errorf("baseline changed by a prompt: %+v", after)
	}
	res := Analyze(goals, nil, nil, prompt)
	if len(res.Assessments) != len(goals) {
		t.Errorf("prompt changed the goal set: %d assessments for %d goals", len(res.Assessments), len(goals))
	}
	if len(res.PromptConflicts) != 1 {
		t.Errorf("Analyze did not surface the conflict: %+v", res.PromptConflicts)
	}
}

// A prompt that agrees with the spec is not a conflict.
func TestPromptAgreementIsNotAConflict(t *testing.T) {
	goals := Requirements(".agents/specs/spec.md", []byte(promptSpec))
	if got := PromptConflicts(goals, "Keep verification separate from implementation status."); len(got) != 0 {
		t.Fatalf("conflicts = %+v, want none", got)
	}
}

// A changed file outside the language set is reported once, not once per side of
// the change, so the diagnostics list stays readable.
//
// The summary names a few files and counts the rest: one line per changed
// stylesheet turns the diagnostics strip into a wall of text.
func TestAlignReportsANonSourceChangeOnce(t *testing.T) {
	change := Change{
		Path:   "internal/serve/static/app.css",
		Status: " M",
		Diff:   "@@ -1,1 +1,2 @@\n .map {}\n+.map-wrap {}\n",
		Old:    []byte(".map {}\n"),
		New:    []byte(".map {}\n.map-wrap {}\n"),
	}
	_, diags := Align(nil, nil, []Change{change})
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one", diags)
	}
	if got := diags[0].Message; got != "1 changed file is outside the V1 language set (internal/serve/static/app.css); their declarations stay unknown" {
		t.Errorf("message = %q", got)
	}
}

// A diff touching many non-source files is one fact, so it is one diagnostic.
func TestAlignSummarisesManyNonSourceChanges(t *testing.T) {
	var changes []Change
	for _, path := range []string{"a.css", "b.html", "c.json", "d.md", "e.yml"} {
		changes = append(changes, Change{Path: path, Status: " M", Diff: "@@ -1 +1 @@\n-x\n+y\n", Old: []byte("x\n"), New: []byte("y\n")})
	}
	_, diags := Align(nil, nil, changes)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %d, want one summary: %+v", len(diags), diags)
	}
	want := "5 changed files are outside the V1 language set (a.css, b.html, c.json, and 2 more); their declarations stay unknown"
	if diags[0].Message != want {
		t.Errorf("message = %q, want %q", diags[0].Message, want)
	}
}

// Vendored code is a dependency, not an unknown: a vendor diff must produce no
// diagnostic at all, however large it is.
func TestAlignSaysNothingAboutVendoredChanges(t *testing.T) {
	changes := []Change{
		{Path: "vendor/github.com/google/uuid/uuid.go", Status: "A ", Diff: "@@ -0,0 +1 @@\n+package uuid\n", New: []byte("package uuid\n")},
		{Path: "web/node_modules/x/index.js", Status: "A ", Diff: "@@ -0,0 +1 @@\n+x\n", New: []byte("x\n")},
	}
	_, diags := Align(nil, nil, changes)
	if len(diags) != 0 {
		t.Fatalf("vendored changes produced %d diagnostics: %+v", len(diags), diags)
	}
}
