package analyze

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"observe/internal/model"
)

// Result is one analysis pass: the per-goal assessment and every place the task
// prompt contradicts a locked goal.
type Result struct {
	Assessments     []model.GoalAssessment
	PromptConflicts []model.PromptConflict
	Diagnostics     []model.Diagnostic
}

// Analyze is the single entry point the composition root calls: link goals to
// code, classify the selected diff, and compare the optional prompt against the
// locked goals.
//
// The axes stay separate. The prompt can only add a conflict record; it cannot
// change the baseline, remove a goal, or move a relation, and no input can make a
// goal verified (AC3, AC5).
func Analyze(goals []model.GoalLocator, decls []Declaration, changes []Change, prompt string) Result {
	assessments, diags := Align(Assess(goals, decls), decls, changes)
	return Result{
		Assessments:     assessments,
		PromptConflicts: PromptConflicts(goals, prompt),
		Diagnostics:     diags,
	}
}

// negationCue are the words that turn a statement into a negation of it.
var negationCue = regexp.MustCompile(`(?i)\b(?:not|never|no|don't|do not|skip|omit|ignore|drop|remove|without|replace)\b`)

// promptWindow bounds how far after a negation cue its term may appear, so a cue
// in an unrelated clause does not reach across the sentence.
const promptWindow = 40

// PromptConflicts reports sentences of a task prompt that contradict a locked
// goal.
//
// The rule is lexical, so it is stated plainly: a sentence contradicts a goal when
// a negation cue governs a term the goal uses — a backticked span, an
// identifier-shaped word such as VerificationEvidence, or a word of six letters or
// more ("verification", "unmapped"). Short words are excluded because "diff",
// "code", and "map" appear in almost every requirement.
//
// The conflict is report-only. The locked spec stays the baseline, the goal stays
// in the assessment set, and nothing is merged or preferred (AC5). The rule can
// still over-report a sentence that happens to negate a term the spec also
// negates; the user closes that by reading the quoted sentence, which is why
// every conflict carries the prompt text and what was compared.
func PromptConflicts(goals []model.GoalLocator, prompt string) []model.PromptConflict {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}

	sentences := sentencesOf(prompt)
	var conflicts []model.PromptConflict
	for _, g := range goals {
		terms := promptTerms(g.Label)
		if len(terms) == 0 {
			continue
		}
		for _, s := range sentences {
			term, ok := negates(s, terms)
			if !ok {
				continue
			}
			conflicts = append(conflicts, model.PromptConflict{
				Locator: g,
				Prompt:  s,
				Diagnostic: fmt.Sprintf(
					"the prompt negates %q, which %s states; unresolved, and the locked spec remains the baseline",
					term, g.SpecPath),
			})
			break // one contradicting sentence is enough to surface the goal
		}
	}
	return conflicts
}

// negates reports the first goal term that a negation cue in sentence governs.
func negates(sentence string, terms []string) (string, bool) {
	lower := strings.ToLower(sentence)
	for _, cue := range negationCue.FindAllStringIndex(lower, -1) {
		end := cue[1] + promptWindow
		if end > len(lower) {
			end = len(lower)
		}
		window := lower[cue[1]:end]
		for _, term := range terms {
			if strings.Contains(window, strings.ToLower(term)) {
				return term, true
			}
		}
	}
	return "", false
}

// sentencesOf splits a prompt into the sentence-sized units a conflict is quoted
// from.
func sentencesOf(prompt string) []string {
	var sentences []string
	split := func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n' || r == ';'
	}
	for _, s := range strings.FieldsFunc(prompt, split) {
		if s = strings.TrimSpace(s); s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// promptTerms are the parts of a requirement a prompt can be said to contradict:
// the spans the author marked as code, identifier-shaped words, and specific
// words long enough to mean something on their own.
func promptTerms(label string) []string {
	seen := make(map[string]bool)
	var terms []string
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" || strings.ContainsAny(text, " \t") || seen[strings.ToLower(text)] {
			return
		}
		seen[strings.ToLower(text)] = true
		terms = append(terms, text)
	}

	for _, m := range backtick.FindAllStringSubmatch(label, -1) {
		add(m[1])
	}
	for _, word := range strings.Fields(label) {
		word = strings.Trim(word, ".,;:()[]'\"`—*")
		if looksLikeCode(word) || isSpecificWord(word) {
			add(word)
		}
	}
	sort.Strings(terms)
	return terms
}

// isSpecificWord reports whether an ordinary word is long enough to be a term on
// its own.
func isSpecificWord(word string) bool {
	letters := []rune(word)
	if len(letters) < 6 {
		return false
	}
	for _, r := range letters {
		if !isLower(r) && !isUpper(r) {
			return false
		}
	}
	return true
}

// looksLikeCode reports whether a word is shaped like an identifier rather than
// an English word.
func looksLikeCode(word string) bool {
	if strings.ContainsRune(word, '_') {
		return true
	}
	letters := []rune(word)
	for i := 1; i < len(letters); i++ {
		if isUpper(letters[i]) && isLower(letters[i-1]) {
			return true // camelCase or PascalCase
		}
	}
	if len(letters) < 2 {
		return false
	}
	for _, r := range letters {
		if !isUpper(r) && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true // an acronym or identifier such as AC3, HTTP, V1
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
