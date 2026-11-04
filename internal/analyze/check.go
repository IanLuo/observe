package analyze

import (
	"path"
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

// Analyze evaluates the checks a locked spec declares and pairs them with the
// goals that own them.
//
// This is the only place a status is produced, and it produces three: pass,
// violated, and unknown. Verification stays a separate axis and is always unknown
// here: no static pass makes code verified (AC3). languageGap counts source files
// the V1 language set cannot read; it turns a would-be violation into an honest
// unknown. The prompt can only add a conflict record; it never moves a verdict
// (AC5).
func Analyze(spec SpecDoc, decls []Declaration, structure Structure, changes []Change, languageGap int, prompt string) Result {
	scan := scanSpec(spec.Path, spec.Body)
	verdicts := evaluate(scan.Checks, decls, structure, languageGap)
	touched := touchedSubjects(changes)

	byGoal := make(map[string][]model.Check, len(scan.Goals))
	for i, raw := range scan.Checks {
		check := verdicts[i]
		check.Touched = isTouched(raw, check, touched)
		byGoal[raw.GoalHash] = append(byGoal[raw.GoalHash], check)
	}

	assessments := make([]model.GoalAssessment, len(scan.Goals))
	for i, goal := range scan.Goals {
		assessments[i] = model.GoalAssessment{
			Locator:      goal,
			Checks:       byGoal[goal.TextHash],
			Verification: model.VerificationUnknown,
		}
	}

	return Result{
		Assessments:     assessments,
		PromptConflicts: PromptConflicts(scan.Goals, prompt),
		Diagnostics:     scan.Diags,
	}
}

// evaluate decides every check against the declarations, the module graph, and the
// import edges.
func evaluate(checks []rawCheck, decls []Declaration, st Structure, languageGap int) []model.Check {
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

	out := make([]model.Check, len(checks))
	for i, c := range checks {
		out[i] = decide(c, modules, symbols, edges, languageGap)
	}
	return out
}

// decide applies one assertion.
//
// A subject that cannot be reached is unknown, not violated: "the code does not
// say this" and "I could not read the code" are different answers, and the third
// verdict exists to keep them apart.
func decide(c rawCheck, modules map[string]Module, symbols map[string][]Declaration, edges map[string][]ModuleEdge, languageGap int) model.Check {
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
	// unreadable makes a missing subject honest when the project holds code this
	// analysis cannot parse.
	unreadable := func(missing string) model.Check {
		if languageGap > 0 {
			return unknown(missing + "; " + plural(languageGap, "source file is", "source files are") + " outside the V1 languages")
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
				return verdict(model.CheckPass, "", nil)
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
				return verdict(model.CheckPass, "", nil)
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
				return verdict(model.CheckPass, "", nil)
			default:
				return verdict(model.CheckViolated, name+" imports "+c.Argument, edgeRefs(found))
			}
		}

	case "file":
		_, exists := modules[path.Dir(name)]
		switch c.Assertion {
		case "exists":
			if exists {
				return verdict(model.CheckPass, "", []model.EvidenceRef{fileRef(name)})
			}
			return unreadable("no file at " + name)
		case "absent":
			if !exists {
				return verdict(model.CheckPass, "", nil)
			}
			return verdict(model.CheckViolated, "file "+name+" still exists", []model.EvidenceRef{fileRef(name)})
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

// LanguageGap counts source files the V1 language set cannot read, so a missing
// symbol can be reported as unknown instead of violated. Vendored and generated
// trees are out of scope, not a gap.
func LanguageGap(paths []string) int {
	gap := 0
	for _, p := range paths {
		if OutOfScope(p) {
			continue
		}
		if _, ok := languageFor(p); ok {
			continue
		}
		if sourceExtension[strings.ToLower(path.Ext(p))] {
			gap++
		}
	}
	return gap
}

// sourceExtension lists extensions that hold source in a language V1 does not
// parse. Anything not here (Markdown, JSON, images) is not a gap: it is not code.
var sourceExtension = map[string]bool{
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".kts": true,
	".swift": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".cs": true, ".php": true, ".scala": true, ".ex": true, ".exs": true, ".erl": true,
	".hs": true, ".ml": true, ".clj": true, ".lua": true, ".dart": true, ".vue": true,
	".svelte": true, ".sql": true, ".sol": true, ".zig": true, ".nim": true,
}
