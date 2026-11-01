package analyze

import (
	"path"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

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
	var (
		decls []Declaration
		diags []model.Diagnostic
	)

	for _, f := range files {
		lang, ok := languageFor(f.Path)
		if !ok {
			diags = append(diags, model.Diagnostic{
				Path:    f.Path,
				Op:      "parse",
				Message: "language is outside the V1 set; relationships stay unknown",
			})
			continue
		}

		found, err := parseDeclarations(lang, f)
		if err != nil {
			diags = append(diags, model.Diagnostic{
				Path:    f.Path,
				Op:      "parse",
				Message: err.Error(),
			})
			continue
		}
		decls = append(decls, found...)
	}

	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Path != decls[j].Path {
			return decls[i].Path < decls[j].Path
		}
		return decls[i].LineStart < decls[j].LineStart
	})
	return decls, diags
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

// languageFor maps a path to one of the V1 languages.
//
// Files under a dependency or vendor directory are excluded: they are third
// party code, so a requirement naming a symbol found there says nothing about
// this project's implementation.
func languageFor(p string) (string, bool) {
	slash := "/" + p
	for _, dir := range []string{"/node_modules/", "/vendor/"} {
		if strings.Contains(slash, dir) {
			return "", false
		}
	}
	lang, ok := sourceLanguage[path.Ext(p)]
	return lang, ok
}

func parseDeclarations(lang string, f File) ([]Declaration, error) {
	var grammar *ts.Language
	switch lang {
	case "go":
		grammar = ts.NewLanguage(tsgo.Language())
	case "typescript":
		if path.Ext(f.Path) == ".tsx" {
			grammar = ts.NewLanguage(tsts.LanguageTSX())
		} else {
			grammar = ts.NewLanguage(tsts.LanguageTypescript())
		}
	default:
		return nil, nil
	}

	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nil, err
	}
	tree := parser.Parse(f.Body, nil)
	defer tree.Close()

	kinds := declarationKinds[lang]
	isTestFile := isTestPath(f.Path)

	var decls []Declaration
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if kinds[n.Kind()] {
			if name := nameOf(n, f.Body); name != "" {
				decls = append(decls, Declaration{
					Name:      name,
					Path:      f.Path,
					LineStart: int(n.StartPosition().Row) + 1,
					LineEnd:   int(n.EndPosition().Row) + 1,
					IsTest:    isTestFile || strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark"),
				})
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return decls, nil
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
