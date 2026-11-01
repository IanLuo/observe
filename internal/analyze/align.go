package analyze

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"observe/internal/model"
)

// Change is one path of the selected diff as the classifier consumes it. It
// mirrors repo.Change: analyze does not import repo, so the composition root
// converts one to the other (.agents/specs/architecture.md, Layers & ownership).
type Change struct {
	Path   string
	Status string // porcelain v1 XY code: " M", "A ", " D", "??", "R "
	Diff   string
	Old    []byte // content at HEAD; nil when the path did not exist there
	New    []byte // content in the working tree; nil when the file is gone
}

// DiffLine is one added or removed line of a unified diff, numbered on the side
// it belongs to. Line numbers are what attribute a change to a declaration.
type DiffLine struct {
	Line  int // 1-based, in the old file for a removal and the new file for an addition
	Text  string
	Added bool
}

// hunkHeader captures the old and new start lines of a hunk.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// Lines parses the added and removed lines out of unified diff text.
//
// Only hunk bodies count, so the `---`/`+++` header lines are not mistaken for a
// removal and an addition of the file itself. When the diff carries no hunk (an
// untracked file, a binary file), the result is empty: the caller has no line
// evidence and must not invent any.
func Lines(diff string) []DiffLine {
	var out []DiffLine
	oldLine, newLine := 0, 0
	inHunk := false

	for _, line := range strings.Split(diff, "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			oldLine++
			newLine++
		case '-':
			out = append(out, DiffLine{Line: oldLine, Text: line[1:]})
			oldLine++
		case '+':
			out = append(out, DiffLine{Line: newLine, Text: line[1:], Added: true})
			newLine++
		case '\\':
			// "\ No newline at end of file" carries no line of its own.
		default:
			inHunk = false // the next file's header
		}
	}
	return out
}

// Align classifies each goal against the selected diff.
//
// The rule is narrow on purpose. The vocabulary has no value for "might matter",
// so a change is only called aligned when this analyzer can point at the code
// that changed:
//
//   - conflict: the diff leaves no declaration of a name the goal uses, while
//     the pre-diff content had one. A move keeps the name and is not a conflict.
//   - appears-to-support: the diff adds lines inside a declaration the goal
//     names, or adds the file that declares it.
//   - unmapped: neither. An untouched goal is never aligned, whatever else the
//     diff does.
//
// Test declarations are excluded from both judgements: changing a test is a
// change to verification evidence, not to implementation, and verification is a
// separate axis that no diff may move (AC3). With no selected diff, the
// relations are returned untouched, because a baseline-only view makes no claim
// about a change.
func Align(assessments []model.GoalAssessment, decls []Declaration, changes []Change) ([]model.GoalAssessment, []model.Diagnostic) {
	if len(changes) == 0 {
		return assessments, nil
	}

	changed := make(map[string]bool, len(changes))
	for _, c := range changes {
		changed[c.Path] = true
	}

	var (
		oldDecls, newDecls []Declaration
		diags              []model.Diagnostic
	)
	for _, c := range changes {
		sides := []struct {
			body []byte
			into *[]Declaration
		}{{c.Old, &oldDecls}, {c.New, &newDecls}}
		for _, side := range sides {
			if side.body == nil {
				continue
			}
			found, parseDiags := Declarations([]File{{Path: c.Path, Body: side.body}})
			// Both sides of one change parse the same path, so the same report would
			// otherwise appear twice and read as two separate problems.
			for _, d := range parseDiags {
				if !containsDiagnostic(diags, d) {
					diags = append(diags, d)
				}
			}
			*side.into = append(*side.into, found...)
		}
	}

	// current is the post-diff tree: unchanged files plus the new side of every
	// change. previous is the pre-diff tree, built the same way from the old
	// sides, which is the only place a removed declaration still exists.
	current := append(append([]Declaration{}, decls...), newDecls...)
	previous := append([]Declaration{}, oldDecls...)
	for _, d := range decls {
		if !changed[d.Path] {
			previous = append(previous, d)
		}
	}

	// The term set spans both trees. A declaration only the pre-diff tree had is
	// still a name the goal can use: without it, a requirement that names the
	// removed code in prose would report no change at all.
	names := nameSet(append(append([]Declaration{}, current...), previous...))
	// The conflict test asks a different question: does the name survive the
	// diff? That is a fact about the post-diff tree alone.
	surviving := nameSet(current)

	out := make([]model.GoalAssessment, len(assessments))
	for i, a := range assessments {
		out[i] = classify(a, changes, current, previous, names, surviving)
	}
	return out, diags
}

// classify applies the rule above to one goal.
func classify(a model.GoalAssessment, changes []Change, current, previous []Declaration, names, surviving map[string]bool) model.GoalAssessment {
	var support, conflict []model.EvidenceRef

	for _, t := range terms(a.Locator.Label, names) {
		for _, d := range previous {
			if d.IsTest || surviving[d.Name] || !matches(t.text, d) {
				continue
			}
			conflict = append(conflict, diffRef(d, t))
		}
		for _, d := range current {
			if d.IsTest || !matches(t.text, d) {
				continue
			}
			if addsLineInside(changes, d) || addsTheFile(changes, d.Path) {
				support = append(support, diffRef(d, t))
			}
		}
	}

	conflict = dedupe(conflict)
	support = dedupe(support)

	switch {
	case len(conflict) > 0:
		a.Relation = model.RelationConflict
		a.Diagnostics = dropNoLink(a.Diagnostics)
		a.Evidence, a.Diagnostics = mergeBounded(a, conflict,
			"conflict: the diff removes "+describeRefs(conflict)+" (pre-diff line numbers), which this goal names")
	case len(support) > 0:
		a.Relation = model.RelationAppearsToSupport
		a.Diagnostics = dropNoLink(a.Diagnostics)
		a.Evidence, a.Diagnostics = mergeBounded(a, support, "")
	case len(a.Evidence) > 0:
		// The goal has a home in the code, and this diff does not go there.
		a.Relation = model.RelationUnmapped
		a.Diagnostics = append(a.Diagnostics, model.Diagnostic{
			Path:    a.Locator.SpecPath,
			Op:      "diff",
			Message: "the diff does not touch the code linked to this goal",
		})
	}
	return a
}

// diffRef is the evidence a classification rests on.
func diffRef(d Declaration, t term) model.EvidenceRef {
	return model.EvidenceRef{
		Path:       d.Path,
		Symbol:     d.Name,
		LineStart:  d.LineStart,
		LineEnd:    d.LineEnd,
		Kind:       model.EvidenceDeclaration,
		Provenance: model.ProvenanceParserRule,
		Confidence: confidence(t, d),
	}
}

// addsLineInside reports whether the diff writes a line inside d's range.
func addsLineInside(changes []Change, d Declaration) bool {
	for _, c := range changes {
		if c.Path != d.Path {
			continue
		}
		for _, l := range Lines(c.Diff) {
			if l.Added && l.Line >= d.LineStart && l.Line <= d.LineEnd {
				return true
			}
		}
	}
	return false
}

// dropNoLink removes the pre-diff "no code link found" diagnostic. Once the diff
// supplies the link, the earlier state is no longer true, and reporting both
// would contradict the evidence list beside it.
func dropNoLink(diags []model.Diagnostic) []model.Diagnostic {
	kept := make([]model.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Op == "link" && d.Message == "no code link found" {
			continue
		}
		kept = append(kept, d)
	}
	return kept
}

// addsTheFile reports whether the change adds the file itself, so every
// declaration in it is new. An untracked file has no diff text to read, which is
// why the status, not the hunk, is the authority here.
func addsTheFile(changes []Change, path string) bool {
	for _, c := range changes {
		if c.Path == path {
			return c.Status == "??" || strings.Contains(c.Status, "A")
		}
	}
	return false
}

// nameSet is the set of declared names, which decides what a term can link to.
func nameSet(decls []Declaration) map[string]bool {
	names := make(map[string]bool, len(decls))
	for _, d := range decls {
		names[d.Name] = true
	}
	return names
}

// mergeBounded adds the diff evidence to what the goal already had and applies
// the shared evidence ceiling.
func mergeBounded(a model.GoalAssessment, extra []model.EvidenceRef, message string) ([]model.EvidenceRef, []model.Diagnostic) {
	diags := a.Diagnostics
	if message != "" {
		diags = append(diags, model.Diagnostic{Path: a.Locator.SpecPath, Op: "diff", Message: message})
	}

	evidence, truncated := boundEvidence(dedupe(append(append([]model.EvidenceRef{}, a.Evidence...), extra...)), a.Locator.SpecPath)
	return evidence, append(diags, truncated...)
}

// describeRefs names the strongest references in a message.
func describeRefs(refs []model.EvidenceRef) string {
	shown := refs
	if len(shown) > 3 {
		shown = shown[:3]
	}
	parts := make([]string, len(shown))
	for i, r := range shown {
		parts[i] = fmt.Sprintf("%s (%s:%d)", r.Symbol, r.Path, r.LineStart)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i] < parts[j] })
	return strings.Join(parts, ", ")
}

// containsDiagnostic reports whether the same report is already collected.
func containsDiagnostic(diags []model.Diagnostic, d model.Diagnostic) bool {
	for _, existing := range diags {
		if existing.Path == d.Path && existing.Op == d.Op && existing.Message == d.Message {
			return true
		}
	}
	return false
}
