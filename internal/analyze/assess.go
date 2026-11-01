package analyze

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"observe/internal/model"
)

// maxEvidencePerGoal bounds one goal's evidence list so the map stays readable.
// Truncation is reported, never silent.
const maxEvidencePerGoal = 12

// backtick matches an inline-code span in a requirement.
var backtick = regexp.MustCompile("`([^`\n]+)`")

// Assess links each goal to candidate implementation evidence.
//
// The link is text-to-symbol matching with provenance, which is why every result
// is marked inferred and why no relation claim is made about support: presence of
// a matching symbol says a goal has a plausible home in the code, not that the
// code does what the goal asks. With no selected diff, the relation stays
// unknown; a goal with no matching symbol reports the AC1 "no code link found"
// state.
//
// Verification is never upgraded here. A discovered test is evidence that a test
// exists, not that it passes or covers this goal, so it is listed separately and
// the verification state stays unknown (AC3).
func Assess(goals []model.GoalLocator, decls []Declaration) []model.GoalAssessment {
	names := make(map[string]bool, len(decls))
	for _, d := range decls {
		names[d.Name] = true
	}

	assessments := make([]model.GoalAssessment, 0, len(goals))
	for _, goal := range goals {
		var implementation, verification []model.EvidenceRef
		for _, t := range terms(goal.Label, names) {
			for _, d := range decls {
				if !matches(t.text, d) {
					continue
				}
				ref := model.EvidenceRef{
					Path:       d.Path,
					Symbol:     d.Name,
					LineStart:  d.LineStart,
					LineEnd:    d.LineEnd,
					Kind:       model.EvidenceDeclaration,
					Provenance: model.ProvenanceExactMatch,
					Confidence: confidence(t, d),
				}
				if d.IsTest {
					ref.Kind = model.EvidenceTest
					verification = append(verification, ref)
					continue
				}
				implementation = append(implementation, ref)
			}
		}

		implementation = dedupe(implementation)
		verification = dedupe(verification)

		assessment := model.GoalAssessment{
			Locator:              goal,
			Relation:             model.RelationUnknown,
			Inferred:             true,
			Evidence:             implementation,
			Verification:         model.VerificationUnknown,
			VerificationEvidence: verification,
		}

		if len(implementation) > maxEvidencePerGoal {
			assessment.Diagnostics = append(assessment.Diagnostics, model.Diagnostic{
				Path:    goal.SpecPath,
				Op:      "link",
				Message: fmt.Sprintf("showing %d of %d code links for this goal", maxEvidencePerGoal, len(implementation)),
			})
			implementation = implementation[:maxEvidencePerGoal]
			assessment.Evidence = implementation
		}
		if len(implementation) == 0 {
			assessment.Relation = model.RelationUnmapped
			assessment.Diagnostics = append(assessment.Diagnostics, model.Diagnostic{
				Path:    goal.SpecPath,
				Op:      "link",
				Message: "no code link found",
			})
		}
		assessments = append(assessments, assessment)
	}
	return assessments
}

// term is a lookup key a requirement offers, and whether the requirement marked
// it as code.
//
// A span in backticks is the author saying "this is a name". A bare word that
// happens to match a declaration is a weaker signal — English verbs collide with
// short function names — so the two must not score the same.
type term struct {
	text string
	code bool
}

// terms are the lookup keys a requirement offers: its inline-code spans, plus any
// word of the text that exactly names a declaration.
//
// Restricting the second set to names that were actually found keeps the match
// precise, so no casing heuristic has to decide what looks like a symbol.
func terms(label string, names map[string]bool) []term {
	seen := make(map[string]bool)
	var out []term
	add := func(text string, code bool) {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		out = append(out, term{text: text, code: code})
	}

	for _, m := range backtick.FindAllStringSubmatch(label, -1) {
		add(m[1], true)
	}
	for _, w := range strings.FieldsFunc(label, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == '/' || isWordRune(r))
	}) {
		if names[w] {
			add(w, false)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].text < out[j].text })
	return out
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// matches reports whether term names a declaration or a directory containing it.
func matches(term string, d Declaration) bool {
	if term == d.Name {
		return true
	}
	dir := strings.TrimSuffix(term, "/")
	return d.Path == dir || strings.HasPrefix(d.Path, dir+"/")
}

// confidence ranks how strongly a term points at a declaration: a backticked
// symbol, then a bare word that matched, then a directory holding it.
func confidence(t term, d Declaration) float64 {
	switch {
	case t.text == d.Name && t.code:
		return 0.9
	case t.text == d.Name:
		return 0.6
	default:
		return 0.5
	}
}

// dedupe drops repeated references to the same declaration, which happens when a
// requirement names both a symbol and the directory it lives in.
func dedupe(refs []model.EvidenceRef) []model.EvidenceRef {
	seen := make(map[string]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		key := fmt.Sprintf("%s:%d:%s", r.Path, r.LineStart, r.Symbol)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}
