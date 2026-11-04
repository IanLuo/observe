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
func analyzeFixture(t *testing.T, spec []byte, files []File, changes []Change, coverage Coverage) Result {
	t.Helper()
	return analyzeDocs(t, []SpecDoc{{Path: "spec.md", Body: spec}}, "spec.md", files, changes, coverage, "")
}

// analyzeDocs runs one pass over several locked documents. The baseline is the one
// whose bullets become goals; the others contribute invariants.
func analyzeDocs(t *testing.T, docs []SpecDoc, baseline string, files []File, changes []Change, coverage Coverage, prompt string) Result {
	t.Helper()
	structure, _ := StructureOf(files)
	decls, _ := Declarations(files)
	return Analyze(Input{
		Baseline:  baseline,
		Docs:      docs,
		Sources:   files,
		Decls:     decls,
		Structure: structure,
		Changes:   changes,
		Coverage:  coverage,
		Prompt:    prompt,
	})
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
	result := analyzeFixture(t, checkSpec("store-exists: symbol A exists"), fixture(), nil, Coverage{})

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
	result := analyzeFixture(t, checkSpec("store-exists: symbol Ledger exists"), fixture(), nil, Coverage{})

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
	result := analyzeFixture(t, checkSpec("store-exists: symbol Ledger exists"), fixture(), nil, Coverage{Readable: 1, Unreadable: 4})

	check := checkNamed(t, result, "store-exists")
	if check.Status != model.CheckUnknown {
		t.Fatalf("status = %q, want unknown", check.Status)
	}
	if !strings.Contains(check.Reason, "outside the V1 languages") {
		t.Errorf("reason %q does not name the gap", check.Reason)
	}
}

func TestCheckOnAMissingModuleIsUnknown(t *testing.T) {
	result := analyzeFixture(t, checkSpec("b-no-reader: module internal/nowhere not-imports internal/b"), fixture(), nil, Coverage{})

	check := checkNamed(t, result, "b-no-reader")
	if check.Status != model.CheckUnknown {
		t.Fatalf("status = %q, want unknown: a module that does not exist cannot be asserted about", check.Status)
	}
}

func TestNotImportsIsViolatedWithTheImportLine(t *testing.T) {
	result := analyzeFixture(t, checkSpec("a-no-b: module internal/a not-imports internal/b"), fixture(), nil, Coverage{})

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
	result := analyzeFixture(t, checkSpec("a-imports-b: module internal/a imports internal/nowhere"), fixture(), nil, Coverage{})

	if got := checkNamed(t, result, "a-imports-b").Status; got != model.CheckViolated {
		t.Fatalf("status = %q, want violated", got)
	}
}

func TestHasTestFollowsTheModuleFiles(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"a-has-test: module internal/a has-test",
		"b-has-test: module internal/b has-test",
	), fixture(), nil, Coverage{})

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
	clean := analyzeFixture(t, spec, fixture(), nil, Coverage{})
	changed := analyzeFixture(t, spec, fixture(), []Change{{Path: "internal/a/a.go"}}, Coverage{})

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
	result := analyzeFixture(t, []byte("## Scope in / out\n- One goal, no check.\n"), fixture(), nil, Coverage{})

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
	), fixture(), nil, Coverage{})

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
	result := analyzeFixture(t, spec, fixture(), nil, Coverage{})

	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "goal bullet") {
		t.Fatalf("diagnostics = %+v, want one about the enclosing goal", result.Diagnostics)
	}
}

func TestDuplicateCheckIDsAreRefused(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"same: symbol A exists",
		"same: symbol Ledger exists",
	), fixture(), nil, Coverage{})

	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "declared twice") {
		t.Fatalf("diagnostics = %+v, want one about the duplicate id", result.Diagnostics)
	}
}

func TestMeasureCountsOnlyRealCode(t *testing.T) {
	paths := []string{
		"internal/a/a.go", "web/main.ts", "docs/spec.md",
		"tools/build.py", "vendor/x/lib.py", "node_modules/y/index.js",
		"packages/db/migrations/0001.sql",
	}
	if got := Measure(paths); got != (Coverage{Readable: 2, Unreadable: 1}) {
		t.Fatalf("Measure = %+v, want two readable and one unreadable", got)
	}
}

// A stray file must not hide a real violation: the caveat is named, the verdict
// stays violated.
func TestSmallUnreadableShareKeepsTheViolation(t *testing.T) {
	result := analyzeFixture(t, checkSpec("store-exists: symbol Ledger exists"), fixture(), nil,
		Coverage{Readable: 40, Unreadable: 3})

	check := checkNamed(t, result, "store-exists")
	if check.Status != model.CheckViolated {
		t.Fatalf("status = %q, want violated", check.Status)
	}
	if !strings.Contains(check.Reason, "not read") {
		t.Errorf("reason %q does not name the unread code", check.Reason)
	}
}

// The subject is the file itself: a path that does not exist under an existing
// module is not present just because its directory is.
func TestFileChecksNameTheFileNotItsDirectory(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"file-there: file internal/a/a.go exists",
		"file-gone: file internal/a/missing.go exists",
		"file-absent: file internal/a/missing.go absent",
	), fixture(), nil, Coverage{})

	if got := checkNamed(t, result, "file-there").Status; got != model.CheckPass {
		t.Errorf("existing file = %q, want pass", got)
	}
	if got := checkNamed(t, result, "file-gone").Status; got != model.CheckViolated {
		t.Errorf("missing file = %q, want violated", got)
	}
	if got := checkNamed(t, result, "file-absent").Status; got != model.CheckPass {
		t.Errorf("absent missing file = %q, want pass", got)
	}
}

// A pass that rests on finding nothing records what it inspected, so the panel can
// show the search instead of an empty list.
func TestAbsencePassesRecordWhatWasInspected(t *testing.T) {
	result := analyzeFixture(t, checkSpec(
		"symbol-absent: symbol Ledger absent",
		"module-absent: module internal/nowhere absent",
	), fixture(), nil, Coverage{})

	for _, id := range []string{"symbol-absent", "module-absent"} {
		check := checkNamed(t, result, id)
		if check.Status != model.CheckPass {
			t.Fatalf("%s status = %q, want pass", id, check.Status)
		}
		if check.Inspected == 0 {
			t.Errorf("%s inspected nothing, so the verdict cannot be shown", id)
		}
		if len(check.Evidence) != 0 {
			t.Errorf("%s carries evidence for an absence: %+v", id, check.Evidence)
		}
	}
}

// A bullet in a locked document outside the goal spec is an invariant: it carries
// checks and verdicts, and it is never counted as a goal (AC14).
func TestChecksOutsideTheSpecBecomeInvariants(t *testing.T) {
	architecture := []byte(`# Architecture
## Layers
- Domain may not import adapters.

  ` + "```check" + `
  domain-pure: module internal/a not-imports internal/b
  ` + "```" + `
- A bullet with no check stays prose.
`)
	result := analyzeDocs(t,
		[]SpecDoc{
			{Path: "spec.md", Body: checkSpec("a-imports-b: module internal/a imports internal/b")},
			{Path: "architecture.md", Body: architecture},
		},
		"spec.md", fixture(), nil, Coverage{}, "")

	var invariants, goals int
	for _, a := range result.Assessments {
		if a.Invariant {
			invariants++
		} else {
			goals++
		}
	}
	if goals != 2 {
		t.Errorf("goals = %d, want the spec's 2 bullets", goals)
	}
	if invariants != 1 {
		t.Fatalf("invariants = %d, want 1: %+v", invariants, result.Assessments)
	}
	for _, a := range result.Assessments {
		if a.Invariant && len(a.Checks) != 1 {
			t.Errorf("invariant lost its check: %+v", a)
		}
		for _, c := range a.Checks {
			if !a.Invariant && c.ID == "domain-pure" {
				t.Error("an invariant check was attached to a goal")
			}
		}
	}
}

// A document that declares no check contributes nothing: its bullets are prose.
func TestDocumentWithoutChecksAddsNoInvariants(t *testing.T) {
	doc := []byte("# Architecture\n## Layers\n- Domain may not import adapters.\n")
	result := analyzeDocs(t,
		[]SpecDoc{{Path: "spec.md", Body: checkSpec("a-imports-b: module internal/a imports internal/b")}, {Path: "architecture.md", Body: doc}},
		"spec.md", fixture(), nil, Coverage{}, "")

	for _, a := range result.Assessments {
		if a.Invariant {
			t.Fatalf("prose became an invariant: %+v", a)
		}
	}
}

// A test that names a check is evidence that the assertion is exercised. It never
// makes the assertion pass, and one covered check does not verify a goal (AC3).
func TestCoversDirectiveIsVerificationEvidenceOnly(t *testing.T) {
	covered := append(fixture(), File{
		Path: "internal/a/covers_test.go",
		Body: []byte("package a\n\n// covers: a-imports-b\nfunc TestLayer(t *testing.T) {}\n"),
	})
	spec := checkSpec("a-imports-b: module internal/a imports internal/b")

	result := analyzeDocs(t, []SpecDoc{{Path: "spec.md", Body: spec}}, "spec.md", covered, nil, Coverage{}, "")
	check := checkNamed(t, result, "a-imports-b")
	if check.Verification != model.VerificationVerified {
		t.Fatalf("verification = %q, want verified", check.Verification)
	}
	if len(check.VerificationEvidence) != 1 || check.VerificationEvidence[0].Path != "internal/a/covers_test.go" {
		t.Fatalf("evidence = %+v, want the test that names it", check.VerificationEvidence)
	}
	if check.VerificationEvidence[0].LineStart != 3 {
		t.Errorf("evidence line = %d, want 3", check.VerificationEvidence[0].LineStart)
	}
	if check.Status != model.CheckPass {
		t.Errorf("status = %q; coverage must not move a verdict", check.Status)
	}

	// One of two checks covered leaves the goal unverified.
	half := checkSpec("a-imports-b: module internal/a imports internal/b", "b-uncovered: symbol B exists")
	partial := analyzeDocs(t, []SpecDoc{{Path: "spec.md", Body: half}}, "spec.md", covered, nil, Coverage{}, "")
	for _, a := range partial.Assessments {
		if len(a.Checks) > 0 && a.Verification != model.VerificationUnknown {
			t.Fatalf("verification = %q with an uncovered check: %+v", a.Verification, a.Checks)
		}
	}

	// No directive, no evidence.
	plain := analyzeDocs(t, []SpecDoc{{Path: "spec.md", Body: spec}}, "spec.md", fixture(), nil, Coverage{}, "")
	if got := checkNamed(t, plain, "a-imports-b").Verification; got != model.VerificationUnknown {
		t.Errorf("verification without a covers directive = %q", got)
	}
}

// A test file is the only place the directive counts: source comments do not verify.
func TestCoversInSourceFilesIsIgnored(t *testing.T) {
	files := append(fixture(), File{
		Path: "internal/a/a.go",
		Body: []byte("package a\n\n// covers: a-imports-b\nfunc A2() {}\n"),
	})
	result := analyzeDocs(t, []SpecDoc{{Path: "spec.md", Body: checkSpec("a-imports-b: module internal/a imports internal/b")}},
		"spec.md", files, nil, Coverage{}, "")

	if got := checkNamed(t, result, "a-imports-b").Verification; got != model.VerificationUnknown {
		t.Fatalf("verification = %q; only a test file may claim coverage", got)
	}
}
