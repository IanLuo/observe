package analyze

import (
	"sort"

	ts "github.com/tree-sitter/go-tree-sitter"

	"observe/internal/model"
)

// fileScan is one file read once: its declarations, its imports, and the identifier
// names it mentions.
//
// Parsing is the expensive part of an analysis, so the walks share one tree and the
// tree is closed before the next file. The previous shape parsed every file up to
// four times — declarations, imports, symbol references, and the declaration count
// inside the structure — and that repetition was 71% of a refresh.
type fileScan struct {
	Decls       []Declaration
	Imports     []importRef
	Identifiers []string
	ParseErr    string
}

// scanFiles parses each file in the V1 language set once.
func scanFiles(files []File) (map[string]fileScan, []model.Diagnostic) {
	scans := make(map[string]fileScan, len(files))
	var diags []model.Diagnostic

	for _, f := range files {
		lang, ok := languageFor(f.Path)
		if !ok {
			// Third-party code is excluded by policy, so silence is the honest answer.
			if !OutOfScope(f.Path) {
				diags = append(diags, model.Diagnostic{
					Path:    f.Path,
					Op:      "parse",
					Message: "outside the V1 language set; its declarations stay unknown",
				})
			}
			continue
		}

		scan := fileScan{}
		tree, err := parse(lang, f)
		if err != nil || tree == nil {
			if err != nil {
				scan.ParseErr = err.Error()
				diags = append(diags, model.Diagnostic{
					Path:    f.Path,
					Op:      "parse",
					Message: err.Error(),
				})
			}
			scans[f.Path] = scan
			continue
		}

		scan.Decls = declarationsIn(lang, tree, f)
		// Tests are counted, not wired: a test file imports the test runner in every
		// module, and that says nothing about how the project is built.
		if !isTestPath(f.Path) {
			scan.Imports = importsIn(tree, f)
			scan.Identifiers = identifiersIn(tree, f)
		}
		tree.Close()
		scans[f.Path] = scan
	}
	return scans, diags
}

// declarationsIn walks one tree for the declarations of a language.
func declarationsIn(lang string, tree *ts.Tree, f File) []Declaration {
	kinds := declarationKinds[lang]
	isTestFile := isTestPath(f.Path)

	var decls []Declaration
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if kinds[n.Kind()] {
			if name := nameOf(n, f.Body); name != "" {
				decls = append(decls, Declaration{
					Name:       name,
					Path:       f.Path,
					LineStart:  int(n.StartPosition().Row) + 1,
					LineEnd:    int(n.EndPosition().Row) + 1,
					IsTest:     isTestFile || isTestName(name),
					Complexity: complexityOf(lang, n, f.Body),
				})
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return decls
}

// declsFrom collects the declarations of every file, in path order.
func declsFrom(files []File, scans map[string]fileScan) []Declaration {
	var decls []Declaration
	for _, f := range files {
		decls = append(decls, scans[f.Path].Decls...)
	}
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Path != decls[j].Path {
			return decls[i].Path < decls[j].Path
		}
		return decls[i].LineStart < decls[j].LineStart
	})
	return decls
}
