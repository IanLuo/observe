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

// rawCheck is a check as written in the spec, before it is evaluated. Its
// GoalHash names the goal bullet that encloses it.
type rawCheck struct {
	ID        string
	SpecPath  string
	GoalHash  string
	Subject   string
	Assertion string
	Argument  string
	Line      int // 1-based line in the spec
}

// specScan is one pass over a locked spec: the goals in it, the checks declared
// in it, and everything it could not read.
type specScan struct {
	Goals  []model.GoalLocator
	Checks []rawCheck
	Diags  []model.Diagnostic
}

// Requirements lists the goal locators in one locked spec.
func Requirements(specPath string, body []byte) []model.GoalLocator {
	return scanSpec(specPath, body).Goals
}

// scanSpec reads goals and checks in one pass, because a check belongs to the goal
// bullet that encloses it: the two cannot be read apart.
//
// A requirement is a top-level list item under a heading outside a context-only
// section, outside fenced code. Only top-level items count: nested bullets are
// detail under a requirement, not requirements of their own. A bullet that is
// nothing but a bold label ("**In V1:**") introduces the nested bullets below it
// and is not itself a requirement.
//
// A ```check block inside a requirement's bullet declares that goal's assertions.
func scanSpec(specPath string, body []byte) specScan {
	var (
		out       specScan
		stack     []section // enclosing headings, outermost first
		fence     bool
		checking  bool
		lastGoal  string
		seenCheck = map[string]bool{}
	)

	for i, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		number := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if fence {
				fence, checking = false, false
				continue
			}
			fence = true
			checking = strings.EqualFold(strings.TrimPrefix(trimmed, "```"), "check")
			continue
		}
		if fence {
			if !checking || trimmed == "" {
				continue
			}
			check, diag := parseCheck(specPath, trimmed, number, lastGoal)
			if diag != nil {
				out.Diags = append(out.Diags, *diag)
				continue
			}
			if seenCheck[check.ID] {
				out.Diags = append(out.Diags, model.Diagnostic{
					Path:    specPath,
					Op:      "check",
					Message: fmt.Sprintf("line %d: check id %q is declared twice; the second is ignored", number, check.ID),
				})
				continue
			}
			seenCheck[check.ID] = true
			out.Checks = append(out.Checks, check)
			continue
		}

		if m := heading.FindStringSubmatch(line); m != nil {
			depth := len(m[1])
			// A heading closes every section at its own depth or deeper, so a
			// second "##" replaces the first rather than nesting under it.
			for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
				stack = stack[:len(stack)-1]
			}
			text := normalize(m[2])
			stack = append(stack, section{depth: depth, text: text, key: sectionKey(text)})
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
		goal := model.GoalLocator{
			SpecPath:     specPath,
			HeadingChain: chain,
			TextHash:     textHash(label),
			Label:        label,
		}
		out.Goals = append(out.Goals, goal)
		lastGoal = goal.TextHash
	}
	return out
}

// checkSubjects are the things a check may name.
var checkSubjects = map[string]bool{"symbol": true, "module": true, "file": true}

// checkAssertions are the assertions a check may make, and whether each needs an
// argument and a module subject.
var checkAssertions = map[string]struct {
	needsArgument bool
	moduleOnly    bool
}{
	"exists":      {},
	"absent":      {},
	"imports":     {needsArgument: true, moduleOnly: true},
	"not-imports": {needsArgument: true, moduleOnly: true},
	"has-test":    {moduleOnly: true},
}

// parseCheck reads one line of a check block: "<id>: <subject> <assertion> [<argument>]".
//
// A line it cannot read becomes a diagnostic, never a guessed check: a check that
// silently disappears would read as a goal with no check defined.
func parseCheck(specPath, line string, number int, goalHash string) (rawCheck, *model.Diagnostic) {
	refuse := func(why string) (rawCheck, *model.Diagnostic) {
		return rawCheck{}, &model.Diagnostic{
			Path:    specPath,
			Op:      "check",
			Message: fmt.Sprintf("line %d: %s: %q", number, why, line),
		}
	}

	id, rest, ok := strings.Cut(line, ":")
	id = strings.TrimSpace(id)
	if !ok || id == "" {
		return refuse("a check needs an id, as in \"store-exists: symbol Store exists\"")
	}
	if goalHash == "" {
		return refuse("a check must sit inside a goal bullet")
	}

	fields := strings.Fields(rest)
	if len(fields) < 3 {
		return refuse("a check needs a subject, an assertion, and the subject's name")
	}
	kind, name, assertion := fields[0], fields[1], fields[2]
	if !checkSubjects[kind] {
		return refuse("unknown subject kind; use symbol, module, or file")
	}
	rule, known := checkAssertions[assertion]
	if !known {
		return refuse("unknown assertion; use exists, absent, imports, not-imports, or has-test")
	}
	if rule.moduleOnly && kind != "module" {
		return refuse(assertion + " applies to a module subject")
	}

	argument := strings.Join(fields[3:], " ")
	if rule.needsArgument && argument == "" {
		return refuse(assertion + " needs a module to name")
	}
	if !rule.needsArgument && argument != "" {
		return refuse(assertion + " takes no argument")
	}
	if argument != "" && strings.Contains(argument, " ") {
		return refuse("an argument names one module")
	}

	return rawCheck{
		ID:        id,
		SpecPath:  specPath,
		GoalHash:  goalHash,
		Subject:   kind + " " + name,
		Assertion: assertion,
		Argument:  argument,
		Line:      number,
	}, nil
}

// section is one enclosing heading and the markdown depth it appeared at.
type section struct {
	depth int
	text  string
	// key is the heading without its section number. The number stays in text,
	// because a locator shows the heading as the document wrote it; only the
	// context-only lookup ignores it.
	key string
}

// sectionNumber matches a leading section number: "1 · ", "2. ", "6) ", "§3 ".
// The separator is required, so a heading that merely starts with digits
// ("100 lines per file") is left alone.
var sectionNumber = regexp.MustCompile(`^(?:§\s*)?\d+(?:\.\d+)*\s*[·.:)–-]\s*`)

// sectionKey is the heading text without its number.
func sectionKey(text string) string {
	return strings.TrimSpace(sectionNumber.ReplaceAllString(text, ""))
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
// section. The lookup ignores section numbers: real documents write
// "## 1 · Problem & who breaks", and a section that goes unrecognised turns its
// prose into goals.
func inContextOnly(stack []section) bool {
	for _, s := range stack {
		if contextOnly[s.key] {
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
