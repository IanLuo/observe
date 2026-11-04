package analyze

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"observe/internal/model"
)

// maxCheckEvidence bounds the references one check carries. A check that matched
// everywhere would flood the payload; the first few name where to look.
const maxCheckEvidence = 5

// SpecDoc is one locked spec, as read from the repository.
type SpecDoc struct {
	Path string
	Body []byte
}

// Result is one analysis pass: each goal with the verdicts of its checks, every
// place the task prompt contradicts a locked goal, and what could not be read.
type Result struct {
	Assessments     []model.GoalAssessment
	PromptConflicts []model.PromptConflict
	Diagnostics     []model.Diagnostic
}

// Input is everything one analysis pass needs.
type Input struct {
	Baseline string    // path of the locked spec that holds the goals; empty when none
	Docs     []SpecDoc // every locked document that may declare checks
	Sources  []File    // source files, read for `covers:` evidence in tests
	Decls    []Declaration
	Structure
	Changes  []Change
	Coverage Coverage
	Prompt   string
}

// Analyze evaluates every check the locked documents declare and pairs each with the
// bullet that owns it.
//
// This is the only place a status is produced, and it produces three: pass,
// violated, and unknown. A bullet in the goal spec is a goal; a bullet elsewhere is
// an invariant and is never counted as a goal (AC14). Verification is a separate
// axis: a test that names a check marks the assertion as exercised, never as
// passing (AC3). The prompt can only add a conflict record; it never moves a verdict
// (AC5).
func Analyze(in Input) Result {
	covers := coverageOf(in.Sources, in.Decls)

	var (
		goals       []model.GoalLocator
		assessments []model.GoalAssessment
		diags       []model.Diagnostic
	)

	for _, doc := range in.Docs {
		scan := scanSpec(doc.Path, doc.Body)
		diags = append(diags, scan.Diags...)
		if len(scan.Checks) == 0 {
			// A document that declares no check contributes no invariants: its bullets
			// are prose, and listing them would bury the ones that carry a verdict.
			if doc.Path == in.Baseline {
				goals = append(goals, scan.Goals...)
				assessments = append(assessments, blank(scan.Goals)...)
			}
			continue
		}

		verdicts := evaluate(scan.Checks, in.Decls, in.Structure, in.Coverage)
		touched := touchedSubjects(in.Changes)

		byGoal := make(map[string][]model.Check, len(scan.Goals))
		for i, raw := range scan.Checks {
			check := verdicts[i]
			check.Touched = isTouched(raw, check, touched)
			if evidence, ok := covers[raw.ID]; ok {
				check.Verification = model.VerificationVerified
				check.VerificationEvidence = evidence
			}
			byGoal[raw.GoalHash] = append(byGoal[raw.GoalHash], check)
		}

		invariant := doc.Path != in.Baseline
		for _, goal := range scan.Goals {
			checks := byGoal[goal.TextHash]
			if invariant && len(checks) == 0 {
				continue // an architecture bullet without a check is prose, not an invariant
			}
			if !invariant {
				goals = append(goals, goal)
			}
			assessments = append(assessments, model.GoalAssessment{
				Locator:              goal,
				Invariant:            invariant,
				Checks:               checks,
				Verification:         verified(checks),
				VerificationEvidence: verificationEvidence(checks),
			})
		}
	}

	return Result{
		Assessments:     assessments,
		PromptConflicts: PromptConflicts(goals, in.Prompt),
		Diagnostics:     diags,
	}
}

// blank is a goal with no check declared: explicit, not a guess.
func blank(goals []model.GoalLocator) []model.GoalAssessment {
	out := make([]model.GoalAssessment, len(goals))
	for i, goal := range goals {
		out[i] = model.GoalAssessment{Locator: goal, Verification: model.VerificationUnknown}
	}
	return out
}

// verified is per goal: every check it declares must be exercised by a test.
// Anything less is unknown — one tested assertion does not verify a goal.
func verified(checks []model.Check) model.Verification {
	if len(checks) == 0 {
		return model.VerificationUnknown
	}
	for _, c := range checks {
		if c.Verification != model.VerificationVerified {
			return model.VerificationUnknown
		}
	}
	return model.VerificationVerified
}

func verificationEvidence(checks []model.Check) []model.EvidenceRef {
	var refs []model.EvidenceRef
	seen := map[string]bool{}
	for _, c := range checks {
		for _, ref := range c.VerificationEvidence {
			key := ref.Path + ":" + string(rune(ref.LineStart))
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

// coversPattern matches the directive a test writes to claim a check.
var coversPattern = regexp.MustCompile(`\bcovers:\s*([a-z0-9][a-z0-9-]*)`)

// testStartPattern recognises the line that opens a test, so a directive can be
// attached from the line above it. TypeScript tests are calls, not declarations, so
// the parser does not hand them over as spans the way it hands over a Go function.
var testStartPattern = regexp.MustCompile(`^\s*(func\s+(Test|Benchmark|Fuzz)\w*|(it|test|describe)(\.\w+)?\s*\()`)

// coverageOf finds the tests that name a check.
//
// The directive counts inside a test declaration, or on the lines directly above
// one. A comment loose in a test file is not a test naming a check, so it is not
// evidence. Naming a check says the assertion is exercised; nothing here runs the
// test, so it never says the assertion holds.
func coverageOf(sources []File, decls []Declaration) map[string][]model.EvidenceRef {
	inTest := map[string][]Declaration{}
	for _, d := range decls {
		if d.IsTest {
			inTest[d.Path] = append(inTest[d.Path], d)
		}
	}

	out := map[string][]model.EvidenceRef{}
	for _, f := range sources {
		if !isTestPath(f.Path) {
			continue
		}
		lines := strings.Split(string(f.Body), "\n")
		for i, line := range lines {
			n := i + 1
			if !onTest(n, lines, inTest[f.Path]) {
				continue
			}
			for _, m := range coversPattern.FindAllStringSubmatch(line, -1) {
				out[m[1]] = append(out[m[1]], model.EvidenceRef{
					Path:       f.Path,
					LineStart:  n,
					LineEnd:    n,
					Kind:       model.EvidenceTest,
					Provenance: model.ProvenanceParserRule,
					Confidence: 1,
					Verifier:   "covers",
				})
			}
		}
	}
	return out
}

// onTest reports whether a directive line belongs to a test: inside one of its
// lines, or immediately above the line that opens it.
func onTest(line int, lines []string, decls []Declaration) bool {
	for _, d := range decls {
		if line >= d.LineStart && line <= d.LineEnd {
			return true
		}
	}
	for next := line + 1; next <= len(lines) && next <= line+2; next++ {
		if strings.TrimSpace(lines[next-1]) == "" {
			continue
		}
		return testStartPattern.MatchString(lines[next-1])
	}
	return false
}

// evaluate decides every check against the declarations, the module graph, and the
// import edges.
func evaluate(checks []rawCheck, decls []Declaration, st Structure, coverage Coverage) []model.Check {
	modules := make(map[string]Module, len(st.Modules))
	for _, m := range st.Modules {
		modules[m.Dir] = m
	}
	symbols := make(map[string][]Declaration, len(decls))
	for _, d := range decls {
		if !d.IsTest {
			symbols[d.Name] = append(symbols[d.Name], d)
		}
	}
	edges := make(map[string][]ModuleEdge, len(st.Modules))
	for _, e := range st.Edges {
		edges[e.From] = append(edges[e.From], e)
	}
	files := map[string]bool{}
	for _, m := range st.Modules {
		for _, f := range m.Files {
			files[f] = true
		}
	}

	out := make([]model.Check, len(checks))
	for i, c := range checks {
		out[i] = decide(c, modules, symbols, edges, files, coverage)
		// Verification starts unknown: no verdict and no coverage by default (AC3).
		out[i].Verification = model.VerificationUnknown
	}
	return out
}

// decide applies one assertion.
//
// A subject that cannot be reached is unknown, not violated: "the code does not
// say this" and "I could not read the code" are different answers, and the third
// verdict exists to keep them apart.
func decide(c rawCheck, modules map[string]Module, symbols map[string][]Declaration, edges map[string][]ModuleEdge, files map[string]bool, coverage Coverage) model.Check {
	kind, name, _ := strings.Cut(c.Subject, " ")
	out := model.Check{
		ID:        c.ID,
		Subject:   c.Subject,
		Assertion: c.Assertion,
		SpecPath:  c.SpecPath,
		Line:      c.Line,
	}

	unknown := func(reason string) model.Check {
		out.Status = model.CheckUnknown
		out.Reason = reason
		return out
	}
	verdict := func(status model.CheckStatus, reason string, evidence []model.EvidenceRef) model.Check {
		out.Status = status
		out.Reason = reason
		out.Evidence = evidence
		return out
	}
	// absent passes have nothing to point at, so they say what they looked at.
	clear := func(inspected int) model.Check {
		out.Status = model.CheckPass
		out.Inspected = inspected
		return out
	}
	// unreadable keeps a missing subject honest: unknown when the unreadable code is
	// a material share of the project, violated with the caveat named when it is a
	// stray corner. Claiming one or the other without saying which would be worse
	// than either.
	unreadable := func(missing string) model.Check {
		if coverage.Material() {
			return unknown(missing + "; " + plural(coverage.Unreadable, "source file is", "source files are") + " outside the V1 languages")
		}
		if coverage.Unreadable > 0 {
			missing += "; " + plural(coverage.Unreadable, "file outside the V1 languages was", "files outside the V1 languages were") + " not read"
		}
		return verdict(model.CheckViolated, missing, nil)
	}

	switch kind {
	case "symbol":
		found := symbols[name]
		switch c.Assertion {
		case "exists":
			if len(found) > 0 {
				return verdict(model.CheckPass, "", symbolRefs(found))
			}
			return unreadable("no declaration named " + name)
		case "absent":
			if len(found) == 0 {
				return clear(len(symbols))
			}
			return verdict(model.CheckViolated, name+" is still declared", symbolRefs(found))
		}

	case "module":
		module, exists := modules[name]
		switch c.Assertion {
		case "exists":
			if exists {
				return verdict(model.CheckPass, "", moduleRefs(module))
			}
			return unreadable("no module at " + name)
		case "absent":
			if !exists {
				return clear(len(modules))
			}
			return verdict(model.CheckViolated, "module "+name+" still exists", moduleRefs(module))
		case "has-test":
			if !exists {
				return unknown("module " + name + " does not exist in the analysed source")
			}
			tests := testFiles(module)
			if len(tests) == 0 {
				return verdict(model.CheckViolated, "no test file in "+name, nil)
			}
			return verdict(model.CheckPass, "", fileRefs(tests))
		case "imports", "not-imports":
			if !exists {
				// A module we cannot see tells us nothing about what it imports.
				return unknown("module " + name + " does not exist in the analysed source")
			}
			found := importEdges(edges[name], c.Argument)
			switch {
			case c.Assertion == "imports" && len(found) > 0:
				return verdict(model.CheckPass, "", edgeRefs(found))
			case c.Assertion == "imports":
				return verdict(model.CheckViolated, name+" imports nothing from "+c.Argument, nil)
			case len(found) == 0:
				return clear(len(module.Files))
			default:
				return verdict(model.CheckViolated, name+" imports "+c.Argument, edgeRefs(found))
			}
		}

	case "file":
		// The subject is the file itself. Reading its directory as the answer made
		// every path under an existing module look present.
		if files[name] {
			switch c.Assertion {
			case "exists":
				return verdict(model.CheckPass, "", []model.EvidenceRef{fileRef(name)})
			case "absent":
				return verdict(model.CheckViolated, "file "+name+" still exists", []model.EvidenceRef{fileRef(name)})
			}
		}
		switch c.Assertion {
		case "exists":
			return unreadable("no file at " + name)
		case "absent":
			return clear(len(files))
		}
	}

	// parseCheck refuses these, so reaching here means the check bypassed it.
	return unknown("assertion " + c.Assertion + " cannot be decided on " + c.Subject)
}

// importEdges lists the edges from one module to the module a check named.
func importEdges(from []ModuleEdge, to string) []ModuleEdge {
	var out []ModuleEdge
	for _, e := range from {
		if e.To == to {
			out = append(out, e)
		}
	}
	return out
}

// symbolRefs points at the declarations that matched, in a stable order.
func symbolRefs(decls []Declaration) []model.EvidenceRef {
	sorted := append([]Declaration(nil), decls...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].LineStart < sorted[j].LineStart
	})
	refs := make([]model.EvidenceRef, 0, len(sorted))
	for _, d := range sorted {
		refs = append(refs, model.EvidenceRef{
			Path:       d.Path,
			Symbol:     d.Name,
			LineStart:  d.LineStart,
			LineEnd:    d.LineEnd,
			Kind:       model.EvidenceDeclaration,
			Provenance: model.ProvenanceParserRule,
			Confidence: 1,
			Verifier:   "check/symbol",
		})
	}
	return bound(refs)
}

// moduleRefs points at the module's files: a directory is not something a reader
// can open.
func moduleRefs(m Module) []model.EvidenceRef { return fileRefs(m.Files) }

// edgeRefs points at the import statement that made the edge, which is the line
// a reader needs to judge the verdict.
func edgeRefs(edges []ModuleEdge) []model.EvidenceRef {
	refs := make([]model.EvidenceRef, 0, len(edges))
	for _, e := range edges {
		refs = append(refs, model.EvidenceRef{
			Path:       e.Path,
			LineStart:  e.Line,
			LineEnd:    e.Line,
			Kind:       model.EvidenceReference,
			Provenance: model.ProvenanceParserRule,
			Confidence: 1,
			Verifier:   "check/import",
		})
	}
	return bound(refs)
}

func fileRefs(files []string) []model.EvidenceRef {
	refs := make([]model.EvidenceRef, 0, len(files))
	for _, f := range files {
		refs = append(refs, fileRef(f))
	}
	return bound(refs)
}

func fileRef(p string) model.EvidenceRef {
	return model.EvidenceRef{
		Path:       p,
		LineStart:  1,
		LineEnd:    1,
		Kind:       model.EvidenceOther,
		Provenance: model.ProvenanceParserRule,
		Confidence: 1,
		Verifier:   "check/file",
	}
}

func bound(refs []model.EvidenceRef) []model.EvidenceRef {
	if len(refs) > maxCheckEvidence {
		return refs[:maxCheckEvidence]
	}
	return refs
}

// testFiles are the files in a module that hold tests, by naming convention.
func testFiles(m Module) []string {
	var out []string
	for _, f := range m.Files {
		if isTestPath(f) {
			out = append(out, f)
		}
	}
	return out
}

// touchedSet is what the selected diff changed, as files and as module
// directories.
type touchedSet struct {
	paths   map[string]bool
	modules map[string]bool
}

func touchedSubjects(changes []Change) touchedSet {
	set := touchedSet{paths: make(map[string]bool, len(changes)), modules: map[string]bool{}}
	for _, c := range changes {
		set.paths[c.Path] = true
		set.modules[path.Dir(c.Path)] = true
	}
	return set
}

// isTouched reports whether the diff changed something the check depends on. It
// marks the check; it never changes a verdict (AC2).
func isTouched(raw rawCheck, check model.Check, touched touchedSet) bool {
	if len(touched.paths) == 0 {
		return false
	}
	kind, name, _ := strings.Cut(raw.Subject, " ")
	switch kind {
	case "module":
		return touched.modules[name]
	case "file":
		return touched.paths[name]
	default:
		for _, ref := range check.Evidence {
			if touched.paths[ref.Path] {
				return true
			}
		}
	}
	return false
}

// Coverage is how much of the project's code the V1 languages can read.
type Coverage struct {
	Readable   int // files in a V1 language
	Unreadable int // files in a programming language V1 cannot parse
}

// Material says whether the unreadable code is a large enough share that a missing
// subject might be hiding in it. A handful of stray files in an otherwise readable
// project must not turn every missing symbol into unknown: that would make the
// verdict useless, which is the failure the third value exists to prevent.
func (c Coverage) Material() bool {
	return c.Unreadable > 0 && c.Unreadable*4 >= c.Readable
}

// Readable reports whether anything could be read at all.
func (c Coverage) ReadableShare() int { return c.Readable }

// Measure counts the code files the V1 language set can and cannot read. Vendored
// and generated trees are out of scope, not a gap. Schemas and migrations are not
// code that declares symbols, so they are not counted.
func Measure(paths []string) Coverage {
	var c Coverage
	for _, p := range paths {
		if OutOfScope(p) {
			continue
		}
		if _, ok := languageFor(p); ok {
			c.Readable++
			continue
		}
		if sourceExtension[strings.ToLower(path.Ext(p))] {
			c.Unreadable++
		}
	}
	return c
}

// sourceExtension lists extensions that hold source in a language V1 does not
// parse. Anything not here (Markdown, JSON, images) is not a gap: it is not code.
var sourceExtension = map[string]bool{
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".kts": true,
	".swift": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".cs": true, ".php": true, ".scala": true, ".ex": true, ".exs": true, ".erl": true,
	".hs": true, ".ml": true, ".clj": true, ".lua": true, ".dart": true, ".vue": true,
	".svelte": true, ".sol": true, ".zig": true, ".nim": true,
}
