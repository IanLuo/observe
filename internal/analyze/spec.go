// Package analyze turns locked-spec text into goal locators and the goal
// baseline (.agents/specs/system-design.md, Analyzer).
//
// It consumes in-memory text only: it does not read files, touch SQLite, or
// serve HTTP, so it can be tested without a repository. Every result is
// deterministic, and an unsupported input is reported as a diagnostic rather
// than an empty success.
package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"observe/internal/model"
)

// Doc is the part of a locked document the baseline decision needs. It is
// defined here, not imported from repo, so analyze keeps its single dependency
// on model.
type Doc struct {
	Path   string
	Family string // "specs" or "design"
	Type   string // "spec", "system-design", "architecture"; empty for design
}

// heading matches an ATX heading and captures its depth and text.
var heading = regexp.MustCompile(`^(#{1,6})[ \t]+(.*)$`)

// Baseline decides whether the locked documents give a usable goal baseline.
//
// The rule is deliberately blunt: goal alignment needs exactly one locked
// `type=spec` document. With none, alignment is unavailable (AC7). With more
// than one, neither is preferred and the conflict is reported unresolved (AC8).
// Choosing between two specs is a product decision this package must not make.
// Other doc types carry context, never the goal baseline.
func Baseline(docs []Doc) model.SpecBaseline {
	var candidates []string
	for _, d := range docs {
		if d.Family == "specs" && d.Type == "spec" {
			candidates = append(candidates, d.Path)
		}
	}

	switch len(candidates) {
	case 1:
		return model.SpecBaseline{State: model.BaselineAvailable, SpecPaths: candidates}
	case 0:
		return model.SpecBaseline{
			State: model.BaselineMissing,
			Diagnostics: []model.Diagnostic{{
				Op:      "baseline",
				Message: "no locked document with type=spec; goal alignment is unavailable",
			}},
		}
	default:
		return model.SpecBaseline{
			State:     model.BaselineConflicting,
			SpecPaths: candidates,
			Diagnostics: []model.Diagnostic{{
				Op:      "baseline",
				Message: fmt.Sprintf("%d locked documents claim the goal baseline (%s); the conflict is unresolved", len(candidates), strings.Join(candidates, ", ")),
			}},
		}
	}
}

// contextOnly names the sections whose bullets are context rather than goals:
// problem framing, measures, assumptions, process, and open questions. The specs
// skill fixes this document shape, so the names are stable across projects.
//
// Every other section contributes goals — including headings this set does not
// name. Excluding by name rather than including by name means a project whose
// spec uses different headings still yields requirements, instead of reporting a
// usable baseline with no goals in it.
var contextOnly = map[string]bool{
	"Problem & who breaks":       true,
	"KPIs & failure signal":      true,
	"Assumptions & dependencies": true,
	"Verification plan":          true,
	"Open questions":             true,
	"Link contract":              true,
}

// leadIn matches the bold phrase that opens many bullets, as in
// "**AC1 — Goal trace:** Given a fixture ...". It is a heading for the sentence
// that follows, not part of the requirement, so it is dropped: leaving it in
// makes a goal's display label start with markup, and makes a heading word like
// "Verification" match a declaration it has nothing to do with.
var leadIn = regexp.MustCompile(`^\*\*[^*]+\*\*[ \t]*:?[ \t]*`)

// Requirements lists the goal locators in one locked spec.
//
// A requirement is a top-level list item under a heading outside a context-only
// section, outside fenced code. Only top-level items count: nested bullets are
// detail under a requirement, not requirements of their own. A bullet that is
// nothing but a bold label ("**In V1:**") introduces the nested bullets below it
// and is not itself a requirement.
func Requirements(specPath string, body []byte) []model.GoalLocator {
	var (
		goals []model.GoalLocator
		stack []section // enclosing headings, outermost first
		fence bool
	)

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}

		if m := heading.FindStringSubmatch(line); m != nil {
			depth := len(m[1])
			// A heading closes every section at its own depth or deeper, so a
			// second "##" replaces the first rather than nesting under it.
			for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, section{depth: depth, text: normalize(m[2])})
			continue
		}

		if !isTopLevelItem(line) || inContextOnly(stack) {
			continue
		}

		label := normalize(leadIn.ReplaceAllString(strings.TrimSpace(line[2:]), ""))
		if label == "" {
			continue
		}
		chain := make([]string, len(stack))
		for i, s := range stack {
			chain[i] = s.text
		}
		goals = append(goals, model.GoalLocator{
			SpecPath:     specPath,
			HeadingChain: chain,
			TextHash:     textHash(label),
			Label:        label,
		})
	}
	return goals
}

// section is one enclosing heading and the markdown depth it appeared at.
type section struct {
	depth int
	text  string
}

// isTopLevelItem reports whether line is an unindented "- " or "* " list item.
func isTopLevelItem(line string) bool {
	if len(line) < 2 {
		return false
	}
	if line[0] != '-' && line[0] != '*' {
		return false
	}
	return line[1] == ' '
}

// inContextOnly reports whether the heading chain sits inside a context-only
// section.
func inContextOnly(stack []section) bool {
	for _, s := range stack {
		if contextOnly[s.text] {
			return true
		}
	}
	return false
}

// normalize collapses whitespace and drops emphasis markers, so restyling
// existing text does not read as a different requirement.
func normalize(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "**", "")), " ")
}

func textHash(text string) string {
	sum := sha256.Sum256([]byte(normalize(text)))
	return hex.EncodeToString(sum[:])
}
