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

// Structure is how a project is built, independent of any goal: the modules it is
// made of, what each one depends on, and what it pulls in from outside.
//
// It answers "what is this project made of", so the map can show the project
// rather than only the parts a goal happens to name. A dependency it cannot
// resolve is not invented: an unresolved import stays external, and an unparsed
// file is a diagnostic (system-design, Failure modes).
type Structure struct {
	Modules []Module
	Edges   []ModuleEdge
	// FileEdges are imports that resolve to one file here, including imports inside
	// a single module: the module graph deliberately hides those, but the drill-down
	// inside a module is exactly that picture.
	FileEdges []FileEdge
}

// FileEdge is a relation between two files of one project.
//
// Kind is "import" when one file imports another, and "reference" when one file
// uses a symbol another file declares — the only relation two Go package files can
// have, since they never import each other. A reference is a name match, so it is
// labelled with the symbol and never presented as proof of anything else.
type FileEdge struct {
	From   string
	To     string
	Kind   string
	Import string // the import path, for an import edge
	Symbol string // the referenced name, for a reference edge
}

// Module is one directory of source: its files, the declarations in them, and its
// dependencies.
type Module struct {
	Dir       string        // repository-relative directory; "." for the root
	Files     []string      // implementation and test files, in path order
	Decls     []Declaration // capped; Truncated says so, so the view never overstates
	DependsOn []string      // module directories this one imports
	Imports   []string      // imports that do not resolve to a module here
	Truncated bool
}

// maxModuleDeclsPerFile keeps one large module from flooding the payload. The view
// shows the first declarations of a file and says the rest are cut, rather than
// presenting a partial list as complete.
const maxModuleDeclsPerFile = 12

// ModuleEdge is one import that resolves inside the project, with the path it
// came from as provenance.
type ModuleEdge struct {
	From   string
	To     string
	Import string
}

// StructureOf reads imports and declarations from the given files.
func StructureOf(files []File) (Structure, []model.Diagnostic) {
	moduleDirs := map[string]bool{}
	fileSet := map[string]bool{}
	for _, f := range files {
		if _, ok := languageFor(f.Path); !ok {
			continue
		}
		moduleDirs[moduleOf(f.Path)] = true
		fileSet[f.Path] = true
	}

	byDir := map[string]*Module{}
	var out Structure
	var diags []model.Diagnostic

	for _, f := range files {
		lang, ok := languageFor(f.Path)
		if !ok {
			continue
		}
		dir := moduleOf(f.Path)
		module := byDir[dir]
		if module == nil {
			module = &Module{Dir: dir}
			byDir[dir] = module
		}
		module.Files = append(module.Files, f.Path)
		// Tests are counted, not wired: a test file imports the test runner in every
		// module, and that says nothing about how the project is built.
		if isTestPath(f.Path) {
			continue
		}

		imports, err := importsOf(lang, f)
		if err != nil {
			diags = append(diags, model.Diagnostic{
				Op:      "structure",
				Message: "imports unreadable in " + f.Path + ": " + err.Error(),
			})
			continue
		}
		for _, raw := range imports {
			if target, targetFile, ok := resolveImport(raw, f.Path, moduleDirs, fileSet); ok {
				if targetFile != "" && targetFile != f.Path {
					out.FileEdges = append(out.FileEdges, FileEdge{From: f.Path, To: targetFile, Kind: "import", Import: raw})
				}
				if target == dir {
					continue // a file importing its own module is not a module edge
				}
				out.Edges = append(out.Edges, ModuleEdge{From: dir, To: target, Import: raw})
				module.DependsOn = append(module.DependsOn, target)
				continue
			}
			module.Imports = append(module.Imports, raw)
		}
	}

	// Declaration counts finish the picture: a module's weight is its symbols, not
	// its line count, and tests are counted apart from implementation.
	var sourceFiles []File
	for _, f := range files {
		if _, ok := languageFor(f.Path); ok {
			sourceFiles = append(sourceFiles, f)
		}
	}
	decls, declDiags := Declarations(sourceFiles)
	diags = append(diags, declDiags...)
	perFile := map[string]int{}
	for _, d := range decls {
		module := byDir[moduleOf(d.Path)]
		if module == nil {
			continue
		}
		if perFile[d.Path] >= maxModuleDeclsPerFile {
			module.Truncated = true
			continue
		}
		perFile[d.Path]++
		module.Decls = append(module.Decls, d)
	}

	out.FileEdges = append(out.FileEdges, referenceEdges(files, decls)...)

	out.Modules = make([]Module, 0, len(byDir))
	for _, module := range byDir {
		module.Files = sortedUnique(module.Files)
		module.DependsOn = sortedUnique(module.DependsOn)
		module.Imports = sortedUnique(module.Imports)
		out.Modules = append(out.Modules, *module)
	}
	sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Dir < out.Modules[j].Dir })
	sort.Slice(out.FileEdges, func(i, j int) bool {
		a, b := out.FileEdges[i], out.FileEdges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Symbol+a.Import < b.Symbol+b.Import
	})
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		if out.Edges[i].To != out.Edges[j].To {
			return out.Edges[i].To < out.Edges[j].To
		}
		return out.Edges[i].Import < out.Edges[j].Import
	})
	return out, diags
}

// maxReferencesPerFile bounds how many symbol references one file contributes, so a
// large file cannot dominate the drill-down.
const maxReferencesPerFile = 24

// referenceEdges links a file to the files of its own module whose symbols it uses.
//
// A dependency between two files of one package is invisible to imports, and it is
// the structure a reader wants when asking how a module is put together. Names are
// matched against declarations in the same module only, so a shared identifier in
// another module never creates an edge.
func referenceEdges(files []File, decls []Declaration) []FileEdge {
	// Per module, so a name declared in two modules cannot cross-link them.
	byModule := map[string]map[string]string{}
	for _, d := range decls {
		dir := moduleOf(d.Path)
		if byModule[dir] == nil {
			byModule[dir] = map[string]string{}
		}
		if _, exists := byModule[dir][d.Name]; !exists {
			byModule[dir][d.Name] = d.Path
		}
	}
	var edges []FileEdge
	for _, f := range files {
		lang, ok := languageFor(f.Path)
		if !ok || isTestPath(f.Path) {
			continue
		}
		dir := moduleOf(f.Path)
		names := byModule[dir]
		if names == nil {
			continue
		}
		used, err := identifiersOf(lang, f)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		count := 0
		for _, name := range used {
			target, declared := names[name]
			if !declared || target == f.Path {
				continue
			}
			key := target + "\x00" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, FileEdge{From: f.Path, To: target, Kind: "reference", Symbol: name})
			count++
			if count >= maxReferencesPerFile {
				break
			}
		}
	}
	return edges
}

// identifiersOf lists the identifier names a file mentions.
func identifiersOf(lang string, f File) ([]string, error) {
	tree, err := parse(lang, f)
	if err != nil || tree == nil {
		return nil, err
	}
	defer tree.Close()

	var names []string
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		switch n.Kind() {
		case "identifier", "type_identifier":
			names = append(names, n.Utf8Text(f.Body))
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return names, nil
}

// moduleOf is the directory a file belongs to.
func moduleOf(p string) string {
	dir := path.Dir(p)
	if dir == "" || dir == "/" {
		return "."
	}
	return dir
}

// resolveImport maps an import to the module it names, when it names one here.
//
// Go imports carry a module path (observe/internal/serve), so a module directory
// that is the import's tail is the target. Relative imports are resolved against
// the importing file. Anything else is external; guessing would be invention.
func resolveImport(raw, filePath string, moduleDirs, fileSet map[string]bool) (module, file string, ok bool) {
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		target := path.Clean(path.Join(path.Dir(filePath), raw))
		if target == ".." || strings.HasPrefix(target, "../") {
			return "", "", false
		}
		// A TypeScript import names a file, not a directory: "./dom" is web/dom.ts.
		for _, candidate := range []string{target, target + ".ts", target + ".tsx", target + "/index.ts"} {
			if fileSet[candidate] {
				return moduleOf(candidate), candidate, true
			}
		}
		if moduleDirs[target] {
			return target, "", true
		}
		return "", "", false
	}

	best := ""
	for dir := range moduleDirs {
		if dir == "." {
			continue
		}
		if raw == dir || strings.HasSuffix(raw, "/"+dir) {
			if len(dir) > len(best) {
				best = dir
			}
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, "", true
}

// importsOf lists the import paths in one file.
func importsOf(lang string, f File) ([]string, error) {
	tree, err := parse(lang, f)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	var imports []string
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		switch n.Kind() {
		case "import_spec":
			if c := n.ChildByFieldName("path"); c != nil {
				imports = append(imports, unquote(c.Utf8Text(f.Body)))
			}
		case "import_statement":
			if c := n.ChildByFieldName("source"); c != nil {
				imports = append(imports, unquote(c.Utf8Text(f.Body)))
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return imports, nil
}

// unquote strips the quotes around an import path.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// parse builds a tree for one file. The caller closes it.
func parse(lang string, f File) (*ts.Tree, error) {
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
	if err := parser.SetLanguage(grammar); err != nil {
		parser.Close()
		return nil, err
	}
	tree := parser.Parse(f.Body, nil)
	parser.Close()
	return tree, nil
}
