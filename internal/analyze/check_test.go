package analyze

import (
	"strings"
	"testing"

	"observe/internal/model"
)

// checkSpec builds a spec whose first goal carries the given check lines.
func checkSpec(checks ...string) []byte {
	var b strings.Builder
	b.WriteString("## Scope in / out\n")
	b.WriteString("- The store keeps one current record.\n")
	b.WriteString("\n")
	b.WriteString("  ```check\n")
	for _, c := range checks {
		b.WriteString("  " + c + "\n")
	}
	b.WriteString("  ```\n")
	b.WriteString("\n")
	b.WriteString("- The analyzer never imports the repository reader.\n")
	return []byte(b.String())
}

// analyzeFixture runs one pass over a small project.
func analyzeFixture(t *testing.T, spec []byte, files []File, changes []Change, languageGap int) Result {
	t.Helper()
	structure, _ := StructureOf(files)
	decls, _ := Declarations(files)
	return Analyze(SpecDoc{Path: "spec.md", Body: spec}, decls, structure, changes, languageGap, "")
}

// checkNamed finds one verdict by id.
func checkNamed(t *testing.T, result Result, id string) model.Check {
	t.Helper()
	for _, a := range result.Assessments {
		for _, c := range a.Checks {
			if c.ID == id {
				return c
			}
		}
	}
	t.Fatalf("no check %q in %+v", id, result.Assessments)
	return model.Check{}
}

func TestCheckPassesWhenTheSymbolExists(t *testing.T) {
	result := analyzeFixture(t, checkSpec("store-exists: symbol A exists"), fixture(), nil, 0)

	check := checkNamed(t, result, "store-exists")
	if check.Status != model.CheckPass {
		t.Fatalf("status = %q (%s), want pass", check.Status, check.Reason)
	}
	if len(check.Evidence) == 0 || check.Evidence[0].Path == "" || check.Evidence[0].LineStart == 0 {
		t.Fatalf("no source evidence: %+v", check.Evidence)
	}
	if check.SpecPath != "spec.md" || check.Line == 0 {
		t.Errorf("check lost its provenance: %+v", check)
	}
}

func TestCheckIsViolatedWhenTheSymbolIsGone(t *testing.T) {
	result := analyzeFixture(t, checkSpec("store-exists: symbol Ledger exists"), fixture(), nil, 0)

	check := checkNamed(t, result, "store-exists")
	if check.Status != model.CheckViolated {
		t.Fatalf("status = %q, want violated", check.Status)
	}
	if check.Reason == "" {
		t.Error("a violation must say what is missing")
	}
}

// A symbol that is not found in a project holding code V1 cannot read is unknown:
// the absence is a limit of the analysis, not a fact about the code.
func TestCheckIsUnknownWhenTheLanguageCannotBeRead(t *testing.T) {
	result := analyzeFixture(t, checkSpec("store-exists: symbol Ledger exists"), fixture(), nil, 2)

	check := checkNamed(t, result, "store-exists")
	if check.Status != model.CheckUnknown {
		t.Fatalf("status = %q, want unknown", check.Status)
	}
	if !strings.Contains(check.Reason, "outside the V1 languages") {
		t.Errorf("reason %q does not name the gap", check.Reason)
	}
}

func TestCheckOnAMissingModuleIsUnknown(t *testing.T) {
	result := analyzeFixture(t, checkSpec("b-no-reader: module internal/nowhere not-imports internal/b"), fixture(), nil, 0)

	check := checkNamed(t, result, "b-no-reader")
	if check.Status != model.CheckUnknown {
		t.Fatalf("status = %q, want unknown: a module that does not exist cannot be asserted about", check.Status)
	}
}

func TestNotImportsIsViolatedWithTheImportLine(t *testing.T) {
	result := analyzeFixture(t, checkSpec("a-no-b: module internal/a not-imports internal/b"), fixture(), nil, 0)

	check := checkNamed(t, result, "a-no-b")
	if check.Status != model.CheckViolated {
		t.Fatalf("status = %q, want violated", check.Status)
	}
	if len(check.Evidence) == 0 {
		t.Fatal("a violation needs the import that broke it")
	}
	if check.Evidence[0].Path != "internal/a/a.go" || check.Evidence[0].LineStart == 0 {
		t.Errorf("evidence = %+v, want the import statement in internal/a/a.go", check.Evidence[0])
	}
}

func TestImportsIsViolatedWhenNothingResolves(t *testing.T) {
	result := analyzeFixture(t, checkSpec("a-imports-b: module internal/a imports internal/nowhere"), fixture(), nil, 0)

	if got := checkNamed(t, result, "a-imports-b").Status; got != model.CheckViolated {
		t.Fatalf("status = %q, want violated", got)
	}
}

func TestHasTestFollowsTheModuleFiles(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"a-has-test: module internal/a has-test",
		"b-has-test: module internal/b has-test",
	), fixture(), nil, 0)

	if got := checkNamed(t, result, "a-has-test").Status; got != model.CheckPass {
		t.Errorf("internal/a status = %q, want pass", got)
	}
	if got := checkNamed(t, result, "b-has-test").Status; got != model.CheckViolated {
		t.Errorf("internal/b status = %q, want violated", got)
	}
}

// A diff marks the checks it touched and never changes a verdict (AC2).
func TestTouchedMarksWithoutChangingTheVerdict(t *testing.T) {
	spec := checkSpec("a-imports-b: module internal/a imports internal/b")
	clean := analyzeFixture(t, spec, fixture(), nil, 0)
	changed := analyzeFixture(t, spec, fixture(), []Change{{Path: "internal/a/a.go"}}, 0)

	if checkNamed(t, clean, "a-imports-b").Touched {
		t.Error("a check is touched with no diff")
	}
	touched := checkNamed(t, changed, "a-imports-b")
	if !touched.Touched {
		t.Error("a change in the check's own module did not mark it")
	}
	if touched.Status != model.CheckPass {
		t.Errorf("status = %q; a diff must not move a verdict", touched.Status)
	}
}

// A goal with no check is explicit, not guessed.
func TestGoalWithoutCheckHasNoVerdict(t *testing.T) {
	result := analyzeFixture(t, []byte("## Scope in / out\n- One goal, no check.\n"), fixture(), nil, 0)

	if len(result.Assessments) != 1 {
		t.Fatalf("assessments = %d, want 1", len(result.Assessments))
	}
	if n := len(result.Assessments[0].Checks); n != 0 {
		t.Fatalf("checks = %d, want 0", n)
	}
	if result.Assessments[0].Verification != model.VerificationUnknown {
		t.Error("verification moved without evidence")
	}
}

// A check line the parser cannot read is reported, never dropped: a dropped check
// would read as a goal with no check defined.
func TestUnreadableChecksBecomeDiagnostics(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"no-id-syntax",
		"bad-assertion: symbol A mentions",
		"bad-subject: widget A exists",
		"passes: symbol A exists",
	), fixture(), nil, 0)

	if len(result.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %d, want 3: %+v", len(result.Diagnostics), result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if !strings.Contains(d.Message, "line ") {
			t.Errorf("diagnostic does not name its line: %q", d.Message)
		}
	}
	if got := checkNamed(t, result, "passes").Status; got != model.CheckPass {
		t.Errorf("the readable check did not survive: %q", got)
	}
}

func TestCheckMustSitInsideAGoal(t *testing.T) {
	spec := []byte("## Scope in / out\n\n```check\nfloating: symbol Store exists\n```\n")
	result := analyzeFixture(t, spec, fixture(), nil, 0)

	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "goal bullet") {
		t.Fatalf("diagnostics = %+v, want one about the enclosing goal", result.Diagnostics)
	}
}

func TestDuplicateCheckIDsAreRefused(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"same: symbol A exists",
		"same: symbol Ledger exists",
	), fixture(), nil, 0)

	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "declared twice") {
		t.Fatalf("diagnostics = %+v, want one about the duplicate id", result.Diagnostics)
	}
}

func TestLanguageGapCountsCodeItCannotRead(t *testing.T) {
	paths := []string{
		"internal/a/a.go", "web/main.ts", "docs/spec.md",
		"tools/build.py", "vendor/x/lib.py", "node_modules/y/index.js",
	}
	if got := LanguageGap(paths); got != 1 {
		t.Fatalf("LanguageGap = %d, want 1 (only tools/build.py)", got)
	}
}
