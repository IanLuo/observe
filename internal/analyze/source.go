package analyze

import (
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"observe/internal/model"
)

// File is one source file offered to the parser.
type File struct {
	Path string
	Body []byte
}

// Declaration is one named definition found in a source file. It is a candidate
// piece of implementation evidence, never proof that a goal is met.
type Declaration struct {
	Name      string
	Path      string
	LineStart int // 1-based, inclusive
	LineEnd   int
	IsTest    bool
	// Complexity is one plus the decision points in this declaration's own syntax.
	// It describes the code, not the requirement: a complex function is not a met
	// goal (AC17).
	Complexity int
}

// declarationKinds are the node kinds that count as an implementation symbol.
// Variable and constant declarations are deliberately excluded: they match far
// too much requirement text to be evidence of anything.
var declarationKinds = map[string]map[string]bool{
	"go": {
		"function_declaration": true,
		"method_declaration":   true,
		"type_spec":            true,
	},
	"typescript": {
		"function_declaration":       true,
		"class_declaration":          true,
		"abstract_class_declaration": true,
		"interface_declaration":      true,
		"type_alias_declaration":     true,
		"enum_declaration":           true,
		"method_definition":          true,
	},
}

// Declarations parses every file and returns the declarations it found.
//
// A file in a language outside the V1 set, or one the parser cannot read, is
// reported as a diagnostic rather than skipped silently: an unparsed file is
// unknown, never an absence of implementation (system-design, Failure modes).
func Declarations(files []File) ([]Declaration, []model.Diagnostic) {
	scans, diags := scanFiles(files)
	return declsFrom(files, scans), diags
}

// Analysis is what one pass over the source files produces.
type Analysis struct {
	Declarations []Declaration
	Structure    Structure
	Diagnostics  []model.Diagnostic
}

// AnalyzeSources parses each file once and derives the declarations and the
// structure from the same trees.
//
// The pipeline takes everything from one call: asking for declarations and then for
// the structure parsed every file twice.
func AnalyzeSources(files []File) Analysis {
	scans, diags := scanFiles(files)
	structure, structureDiags := structureFrom(files, scans)

	return Analysis{
		Declarations: declsFrom(files, scans),
		Structure:    structure,
		Diagnostics:  append(diags, structureDiags...),
	}
}

// sourceLanguage maps a V1 source extension to its grammar. The V1 language set
// is Go and TypeScript (.agents/specs/architecture.md D2); this table is the one
// place that decides it, so adding a language is a single edit.
var sourceLanguage = map[string]string{
	".go":  "go",
	".ts":  "typescript",
	".tsx": "typescript",
}

// IsSourcePath reports whether a path is in the V1 language set. Callers use it
// to decide what to hand the parser.
func IsSourcePath(p string) bool {
	_, ok := sourceLanguage[path.Ext(p)]
	return ok
}

// OutOfScope reports whether a path is third-party code this analyzer never reads.
//
// A vendor or dependency directory is a policy exclusion, not an unknown: it is
// this project's dependency, not its implementation, so its files are neither
// parsed nor reported. Saying "unknown" about every vendored file would bury the
// files that matter.
func OutOfScope(p string) bool {
	slash := "/" + p
	for _, dir := range []string{"/node_modules/", "/vendor/"} {
		if strings.Contains(slash, dir) {
			return true
		}
	}
	return false
}

// languageFor maps a path to one of the V1 languages.
func languageFor(p string) (string, bool) {
	if OutOfScope(p) {
		return "", false
	}
	lang, ok := sourceLanguage[path.Ext(p)]
	return lang, ok
}

// nameOf reads a node's declared name. Grammar fields are used first; a
// type_spec inside a grouped declaration has no name field of its own, so its
// first identifier is used instead.
func nameOf(n *ts.Node, src []byte) string {
	if c := n.ChildByFieldName("name"); c != nil {
		return c.Utf8Text(src)
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c.Kind() == "type_identifier" || c.Kind() == "identifier" || c.Kind() == "property_identifier" {
			return c.Utf8Text(src)
		}
	}
	return ""
}

// isTestName reports whether a declaration name is a test by convention.
func isTestName(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark")
}

// isTestPath reports whether a path marks its declarations as tests.
func isTestPath(p string) bool {
	base := path.Base(p)
	for _, suffix := range []string{"_test.go", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}
