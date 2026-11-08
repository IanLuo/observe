package analyze

// Request flow: which tables a route can reach.
//
// The chain runs through four things a reader can check by hand: a route
// registration, the use case its handler calls, the factory that receives the store,
// and the store method whose SQL names a table. Each hop is a call in the source, so
// every hop carries its file and line. When a binding is not the shape the rules
// describe, the flow stops there and says which step stopped it — it never guesses a
// table (AC19).
//
// The rules are deliberately narrow. A factory argument counts only when the factory
// passes it, unchanged, into a method it returns. A store class counts only when a
// composition-root constant is assigned its `new` expression and that constant is
// passed by name. Every other binding is unknown.

import (
	"regexp"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// Flow is one route and what it can reach.
type Flow struct {
	Method string
	Route  string // the registered path
	Path   string // file that registered the route
	Line   int
	Hops   []FlowHop
	// Table lists the tables the reached store method names in literal SQL. Empty
	// means the reader found none, and Unknown says why.
	Table []string
	// Unknown names the step that stopped the chain, or is empty when it completed.
	Unknown string
}

// FlowHop is one step, with the source location a reader can open.
type FlowHop struct {
	Name   string // "route", "use case", "store method", or "table"
	Detail string
	Path   string
	Line   int
}

// routeMethods are the registration calls this reader models.
var routeMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
	"head": true, "options": true, "all": true,
}

// tableInSQL matches the table a statement names after FROM, INTO, UPDATE, or JOIN.
var tableInSQL = regexp.MustCompile(`(?i)\b(?:from|into|update|join)\s+([a-z_][a-z0-9_]*)`)

// sqlKeywords are words that can follow the keyword but are not table names.
var sqlKeywords = map[string]bool{
	"select": true, "where": true, "values": true, "set": true, "on": true,
	"using": true, "lateral": true, "only": true, "table": true, "if": true,
	"exists": true, "returning": true, "default": true,
}

// fileFacts is what one file contributes to a flow, read once from its tree.
type fileFacts struct {
	path        string
	newConsts   map[string]newBinding // const offers = new PostgresOfferStore(pool)
	invocations []invocation          // createServer({ offers })
	routes      []routeSite
	factories   map[string]*factoryDef
	classes     map[string]*classDef
	constCalls  map[string]constCall
	// params maps a function name to its destructured parameter keys, so a server
	// option passed by name can be traced to the constant the caller supplied.
	params map[string]map[string]string
}

// constCall is `const x = factory({ ... })` in one file.
type constCall struct {
	callee string
	args   map[string]string
	scope  string
	path   string
}

type newBinding struct {
	class string
	line  int
}

type invocation struct {
	callee string
	args   map[string]string // object key to the identifier passed
	scope  string            // the function the call sits in
	path   string
	line   int
}

type routeSite struct {
	method string
	path   string
	line   int
	calls  []callSite // member calls inside the handler
}

type callSite struct {
	receiver string
	method   string
	path     string
	line     int
}

type factoryDef struct {
	name    string
	path    string
	line    int
	params  map[string]string // local name → the argument key it came from
	methods map[string]*methodDef
	// indirect is set when the body binds a parameter to something other than a
	// direct call on it, which the rules do not follow.
	indirect map[string]string
}

type methodDef struct {
	line   int
	calls  []callSite
	tables []string
}

type classDef struct {
	name    string
	path    string
	line    int
	methods map[string]*methodDef
}

// RequestFlows reads every file once and returns the flow of each registered route,
// in path order.
func RequestFlows(files []File) []Flow {
	facts := map[string]*fileFacts{}
	for _, f := range files {
		lang, ok := languageFor(f.Path)
		if !ok || lang != "typescript" || isTestPath(f.Path) {
			continue
		}
		tree, err := parse(lang, f)
		if err != nil || tree == nil {
			continue
		}
		facts[f.Path] = readFacts(f, tree)
		tree.Close()
	}

	// Names are resolved across the repository by uniqueness: a factory or class
	// declared twice is not a name this reader can follow, so it stays unknown.
	factories := map[string]*factoryDef{}
	classes := map[string]*classDef{}
	for _, path := range sortedKeys(facts) {
		f := facts[path]
		for name, def := range f.factories {
			factories[name] = def
		}
		for name, def := range f.classes {
			classes[name] = def
		}
	}

	var flows []Flow
	for _, path := range sortedKeys(facts) {
		f := facts[path]
		for _, route := range f.routes {
			flows = append(flows, resolveFlow(f, route, facts, factories, classes))
		}
	}
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].Path != flows[j].Path {
			return flows[i].Path < flows[j].Path
		}
		return flows[i].Line < flows[j].Line
	})
	return flows
}

// resolveFlow follows one route through the bindings, appending a hop per step.
func resolveFlow(f *fileFacts, route routeSite, facts map[string]*fileFacts, factories map[string]*factoryDef, classes map[string]*classDef) Flow {
	flow := Flow{Method: strings.ToUpper(route.method), Route: route.path, Path: f.path, Line: route.line}
	flow.Hops = append(flow.Hops, FlowHop{
		Name: "route", Detail: flow.Method + " " + route.path, Path: f.path, Line: route.line,
	})

	// The use case: a call on a local bound to a factory call in this file.
	call, factoryCall, ok := useCaseCall(f, route)
	if !ok {
		flow.Unknown = "no call on a factory-built use case in this handler"
		return flow
	}
	flow.Hops = append(flow.Hops, FlowHop{
		Name: "use case", Detail: call.receiver + "." + call.method, Path: call.path, Line: call.line,
	})

	factory, ok := factories[factoryCall.callee]
	if !ok {
		flow.Unknown = "factory " + factoryCall.callee + " is not declared in this repository"
		return flow
	}
	method, ok := factory.methods[call.method]
	if !ok {
		flow.Unknown = "factory " + factory.name + " returns no method " + call.method
		return flow
	}
	if reason, ok := factory.indirect["store"]; ok {
		flow.Unknown = "factory " + factory.name + " binds its store argument indirectly (" + reason + ")"
		return flow
	}

	// The store method: a call on the parameter the factory received as "store".
	var storeCall *callSite
	for i, c := range method.calls {
		if c.receiver == factory.params["store"] {
			storeCall = &method.calls[i]
			break
		}
	}
	if storeCall == nil {
		flow.Unknown = "factory " + factory.name + "." + call.method + " does not call its store argument"
		return flow
	}
	flow.Hops = append(flow.Hops, FlowHop{
		Name: "store method", Detail: "store." + storeCall.method, Path: storeCall.path, Line: storeCall.line,
	})

	// The concrete class: a composition-root constant passed by name.
	class, reason := resolveClass(f, factoryCall, facts, classes)
	if class == nil {
		flow.Unknown = reason
		return flow
	}
	definition, ok := class.methods[storeCall.method]
	if !ok {
		flow.Unknown = "class " + class.name + " has no method " + storeCall.method
		return flow
	}

	seen := map[string]bool{}
	for _, table := range definition.tables {
		if seen[table] {
			continue
		}
		seen[table] = true
		flow.Table = append(flow.Table, table)
		flow.Hops = append(flow.Hops, FlowHop{
			Name: "table", Detail: table, Path: class.path, Line: definition.line,
		})
	}
	if len(flow.Table) == 0 {
		flow.Unknown = "class " + class.name + "." + storeCall.method + " names no table in literal SQL"
	}
	return flow
}

// useCaseCall finds the first call in the route handler on a local bound to a factory
// call in the same file, and the factory call itself.
func useCaseCall(f *fileFacts, route routeSite) (callSite, factoryBinding, bool) {
	bindings := factoryBindingsOf(f)
	for _, c := range route.calls {
		if b, ok := bindings[c.receiver]; ok {
			return c, b, true
		}
	}
	return callSite{}, factoryBinding{}, false
}

// factoryBinding is `const x = createSomething({ ... })` in one file.
type factoryBinding struct {
	callee string
	args   map[string]string
	scope  string // the enclosing function, for the composition-root binding
}

func factoryBindingsOf(f *fileFacts) map[string]factoryBinding {
	out := map[string]factoryBinding{}
	for name, inv := range f.constCalls {
		out[name] = factoryBinding{callee: inv.callee, args: inv.args, scope: inv.scope}
	}
	return out
}

// resolveClass finds the class a store argument names.
//
// Two shapes count: a constant in this file assigned a `new` expression, and a
// parameter of the enclosing function filled by a call site that passes a constant
// assigned a `new` expression. Anything else does not identify a class.
func resolveClass(f *fileFacts, binding factoryBinding, facts map[string]*fileFacts, classes map[string]*classDef) (*classDef, string) {
	ident, ok := binding.args["store"]
	if !ok {
		return nil, "the factory receives no store argument at its call site"
	}

	// A constant in this file: `const offers = new PostgresOfferStore(pool)`.
	if nb, ok := f.newConsts[ident]; ok {
		return classNamed(nb.class, classes)
	}

	// Otherwise the identifier is a parameter of the enclosing function. Its key is
	// the option name the caller supplies, and the caller's argument is the constant.
	key := "store"
	if params, ok := f.params[binding.scope]; ok {
		for k, local := range params {
			if local == ident {
				key = k
			}
		}
	}
	for _, path := range sortedKeys(facts) {
		g := facts[path]
		for _, inv := range g.invocations {
			if inv.callee != binding.scope || binding.scope == "" {
				continue
			}
			name, ok := inv.args[key]
			if !ok {
				continue
			}
			if nb, ok := g.newConsts[name]; ok {
				return classNamed(nb.class, classes)
			}
			// One more step would need a second parameter chain; stop and say so
			// rather than guess which constant reaches this one.
			return nil, "the store " + name + " passed into " + binding.scope + " is not a new expression in " + path
		}
	}
	return nil, "the store " + ident + " is not constructed with a new expression this reader can see"
}

// classNamed returns a class by name, or the reason it cannot be followed.
func classNamed(name string, classes map[string]*classDef) (*classDef, string) {
	class, ok := classes[name]
	if !ok {
		return nil, "class " + name + " is not declared in this repository"
	}
	return class, ""
}

// ---------------------------------------------------------------------------
// Reading one file

func readFacts(f File, tree *ts.Tree) *fileFacts {
	facts := &fileFacts{
		path:       f.Path,
		newConsts:  map[string]newBinding{},
		factories:  map[string]*factoryDef{},
		classes:    map[string]*classDef{},
		constCalls: map[string]constCall{},
		params:     map[string]map[string]string{},
	}
	var walk func(n *ts.Node, scope string)
	walk = func(n *ts.Node, scope string) {
		switch n.Kind() {
		case "function_declaration":
			name := nameOf(n, f.Body)
			if def := readFactory(n, f, name); def != nil {
				facts.factories[name] = def
			}
			if name != "" {
				if params := field(n, "parameters", f.Body); params != nil {
					facts.params[name] = destructuredParams(params, f.Body)
				}
			}
			// Children only: walking this node again would read the same declaration
			// forever.
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), name)
			}
			return
		case "class_declaration":
			name := nameOf(n, f.Body)
			if name != "" {
				facts.classes[name] = readClass(n, f, name)
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), scope)
			}
			return
		case "lexical_declaration":
			readLexical(n, f, scope, facts)
		case "call_expression":
			readCall(n, f, scope, facts)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i), scope)
		}
	}
	walk(tree.RootNode(), "")
	return facts
}

// readLexical records `const x = new Class(...)` and `const x = factory({ ... })`.
func readLexical(n *ts.Node, f File, scope string, facts *fileFacts) {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		decl := n.NamedChild(i)
		if decl.Kind() != "variable_declarator" {
			continue
		}
		name := fieldText(decl, "name", f.Body)
		value := field(decl, "value", f.Body)
		if name == "" || value == nil {
			continue
		}
		switch value.Kind() {
		case "new_expression":
			if ctor := fieldText(value, "constructor", f.Body); ctor != "" {
				facts.newConsts[name] = newBinding{class: ctor, line: int(decl.StartPosition().Row) + 1}
			}
		case "call_expression":
			callee := fieldText(value, "function", f.Body)
			args := objectArgs(value, f.Body)
			if callee != "" && args != nil {
				facts.constCalls[name] = constCall{callee: callee, args: args, scope: scope, path: f.Path}
				facts.invocations = append(facts.invocations, invocation{
					callee: callee, args: args, path: f.Path, line: int(value.StartPosition().Row) + 1,
				})
			}
		}
	}
}

// readCall records a route registration and an invocation with an object argument.
func readCall(n *ts.Node, f File, scope string, facts *fileFacts) {
	fn := calleeOf(n, f.Body)
	if fn == nil {
		return
	}
	if fn.Kind() == "identifier" {
		// createServer({ offers }) — the composition-root call.
		if args := objectArgs(n, f.Body); args != nil {
			facts.invocations = append(facts.invocations, invocation{
				callee: fn.Utf8Text(f.Body), args: args, scope: scope,
				path: f.Path, line: int(n.StartPosition().Row) + 1,
			})
		}
		return
	}
	if fn.Kind() != "member_expression" {
		return
	}
	receiver := fieldText(fn, "object", f.Body)
	method := fieldText(fn, "property", f.Body)
	if receiver == "" || method == "" {
		return
	}

	args := field(n, "arguments", f.Body)
	if args == nil {
		return
	}

	if routeMethods[method] {
		if path, ok := firstStringArg(args, f.Body); ok && strings.HasPrefix(path, "/") {
			handler := args.NamedChild(uint(args.NamedChildCount()) - 1)
			route := routeSite{method: method, path: path, line: int(n.StartPosition().Row) + 1}
			if handler != nil && handler.Kind() != "string" {
				route.calls = memberCallsIn(handler, f)
			}
			facts.routes = append(facts.routes, route)
		}
	}

	// createServer({ offers }) — an invocation whose object argument names constants.
	if list := objectArgs(n, f.Body); list != nil {
		facts.invocations = append(facts.invocations, invocation{
			callee: receiver + "." + method, args: list, scope: scope,
			path: f.Path, line: int(n.StartPosition().Row) + 1,
		})
		facts.invocations = append(facts.invocations, invocation{
			callee: receiver, args: list, scope: scope, path: f.Path, line: int(n.StartPosition().Row) + 1,
		})
	}
}

// memberCallsIn collects `receiver.method(...)` calls inside a handler body.
func memberCallsIn(handler *ts.Node, f File) []callSite {
	var calls []callSite
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if n.Kind() == "call_expression" {
			if fn := calleeOf(n, f.Body); fn != nil && fn.Kind() == "member_expression" {
				receiver := fieldText(fn, "object", f.Body)
				method := fieldText(fn, "property", f.Body)
				if receiver != "" && method != "" {
					calls = append(calls, callSite{
						receiver: receiver, method: method,
						path: f.Path, line: int(n.StartPosition().Row) + 1,
					})
				}
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(handler)
	return calls
}

// readFactory reads a function that returns an object literal of methods.
func readFactory(n *ts.Node, f File, name string) *factoryDef {
	if name == "" {
		return nil
	}
	body := field(n, "body", f.Body)
	if body == nil {
		return nil
	}
	returned := returnedObject(body, f)
	if returned == nil {
		return nil
	}

	def := &factoryDef{
		name: name, path: f.Path, line: int(n.StartPosition().Row) + 1,
		params: map[string]string{}, methods: map[string]*methodDef{}, indirect: map[string]string{},
	}
	if params := field(n, "parameters", f.Body); params != nil {
		def.params = destructuredParams(params, f.Body)
	}
	declared := functionsIn(body, f)
	for i := uint(0); i < returned.NamedChildCount(); i++ {
		member := returned.NamedChild(i)
		if member.Kind() == "method_definition" {
			methodName := nameOf(member, f.Body)
			if methodName == "" {
				continue
			}
			def.methods[methodName] = &methodDef{
				line:  int(member.StartPosition().Row) + 1,
				calls: memberCallsIn(member, f),
			}
			continue
		}
		// `return { getPublicItem, … }` publishes a function declared in the body.
		if member.Kind() == "shorthand_property_identifier" {
			name := member.Utf8Text(f.Body)
			if fn, ok := declared[name]; ok {
				def.methods[name] = &methodDef{line: int(fn.StartPosition().Row) + 1, calls: memberCallsIn(fn, f)}
			}
		}
	}
	def.indirect = indirectBindings(body, def.params, f)
	return def
}

// returnedObject finds the object literal a factory publishes.
//
// A factory body can return several objects — one per helper — so the published one
// is the literal with the most methods. Choosing the first return reported a helper's
// object as the factory's API, which made every method look like it skipped the store.
func returnedObject(body *ts.Node, f File) *ts.Node {
	var best *ts.Node
	bestMethods := -1
	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child.Kind() != "return_statement" {
			continue
		}
		value := child.NamedChild(0)
		if value == nil || value.Kind() != "object" {
			continue
		}
		methods := 0
		for j := uint(0); j < value.NamedChildCount(); j++ {
			if kind := value.NamedChild(j).Kind(); kind == "method_definition" || kind == "shorthand_property_identifier" {
				methods++
			}
		}
		if methods > 0 && methods >= bestMethods {
			best, bestMethods = value, methods
		}
	}
	return best
}

// functionsIn maps each function declared in a body to its node, so a shorthand
// property in the returned object resolves to the function it names.
func functionsIn(body *ts.Node, f File) map[string]*ts.Node {
	out := map[string]*ts.Node{}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child.Kind() == "function_declaration" {
			if name := nameOf(child, f.Body); name != "" {
				out[name] = child
			}
		}
	}
	return out
}

// indirectBindings names each parameter the body re-binds instead of calling on it.
// "store.createPlan" is followed; "saved = store" is not, because the reader would
// have to reason about when that assignment runs.
func indirectBindings(body *ts.Node, params map[string]string, f File) map[string]string {
	locals := map[string]bool{}
	for _, local := range params {
		locals[local] = true
	}
	reasons := map[string]string{}
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if n.Kind() == "assignment_expression" {
			left := field(n, "left", f.Body)
			right := field(n, "right", f.Body)
			if left != nil && right != nil && locals[right.Utf8Text(f.Body)] {
				reasons[right.Utf8Text(f.Body)] = "assigned to " + left.Utf8Text(f.Body)
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(body)
	byKey := map[string]string{}
	for key, local := range params {
		if reason, ok := reasons[local]; ok {
			byKey[key] = reason
		}
	}
	return byKey
}

// destructuredParams maps each object-pattern key to the local name it binds.
func destructuredParams(params *ts.Node, src []byte) map[string]string {
	out := map[string]string{}
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		switch n.Kind() {
		case "object_pattern":
			for i := uint(0); i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				switch child.Kind() {
				case "shorthand_property_identifier_pattern":
					out[child.Utf8Text(src)] = child.Utf8Text(src)
				case "pair_pattern":
					key := fieldText(child, "key", src)
					value := fieldText(child, "value", src)
					if key != "" && value != "" {
						out[key] = value
					}
				}
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(params)
	return out
}

// readClass reads a class's methods and the tables their literal SQL names.
func readClass(n *ts.Node, f File, name string) *classDef {
	def := &classDef{name: name, path: f.Path, line: int(n.StartPosition().Row) + 1, methods: map[string]*methodDef{}}
	body := field(n, "body", f.Body)
	if body == nil {
		return def
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		method := body.NamedChild(i)
		if method.Kind() != "method_definition" {
			continue
		}
		methodName := nameOf(method, f.Body)
		if methodName == "" {
			continue
		}
		def.methods[methodName] = &methodDef{
			line:   int(method.StartPosition().Row) + 1,
			tables: tablesIn(method, f),
		}
	}
	return def
}

// tablesIn lists the tables named by the literal SQL of one method body.
//
// Only a literal SQL string counts: a query built from a variable could name any
// table, so it contributes none and the flow says the method names no table.
func tablesIn(n *ts.Node, f File) []string {
	var tables []string
	seen := map[string]bool{}
	var walk func(node *ts.Node)
	walk = func(node *ts.Node) {
		if node.Kind() == "call_expression" {
			if fn := calleeOf(node, f.Body); fn != nil && fn.Kind() == "member_expression" && fieldText(fn, "property", f.Body) == "query" {
				args := field(node, "arguments", f.Body)
				if args != nil && args.NamedChildCount() > 0 {
					if text, ok := firstStringArg(args, f.Body); ok {
						for _, m := range tableInSQL.FindAllStringSubmatchIndex(text, -1) {
							table := strings.ToLower(text[m[2]:m[3]])
							if sqlKeywords[table] || seen[table] {
								continue
							}
							// "FOR UPDATE OF x" locks a row; it does not name a table.
							if strings.HasSuffix(strings.ToLower(strings.TrimSpace(text[:m[0]])), "for") {
								continue
							}
							seen[table] = true
							tables = append(tables, table)
						}
					}
				}
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	return tables
}

// ---------------------------------------------------------------------------
// Small helpers

// calleeOf unwraps the nodes that can sit in front of a call before the callee
// itself. `await store.save(...)` is parsed with the await_expression wrapping the
// member expression, so reading the field alone would miss every awaited call.
func calleeOf(call *ts.Node, src []byte) *ts.Node {
	fn := field(call, "function", src)
	for fn != nil {
		switch fn.Kind() {
		case "await_expression", "parenthesized_expression", "non_null_expression":
			next := fn.NamedChild(0)
			if next == nil {
				return nil
			}
			fn = next
		default:
			return fn
		}
	}
	return nil
}

// field reads a node-field as text, or the node when the field is a node.
func field(n *ts.Node, name string, src []byte) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name)
}

func fieldText(n *ts.Node, name string, src []byte) string {
	child := field(n, name, src)
	if child == nil {
		return ""
	}
	return child.Utf8Text(src)
}

// objectArgs reads an object-literal argument as key → identifier.
func objectArgs(call *ts.Node, src []byte) map[string]string {
	args := field(call, "arguments", src)
	if args == nil {
		return nil
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		if arg.Kind() != "object" {
			continue
		}
		out := map[string]string{}
		for j := uint(0); j < arg.NamedChildCount(); j++ {
			entry := arg.NamedChild(j)
			switch entry.Kind() {
			case "shorthand_property_identifier":
				name := entry.Utf8Text(src)
				out[name] = name
			case "pair":
				key := fieldText(entry, "key", src)
				value := field(entry, "value", src)
				if key != "" && value != nil && value.Kind() == "identifier" {
					out[key] = value.Utf8Text(src)
				}
			}
		}
		return out
	}
	return nil
}

// firstStringArg reads the first string or template argument as text.
func firstStringArg(args *ts.Node, src []byte) (string, bool) {
	if args == nil || args.NamedChildCount() == 0 {
		return "", false
	}
	first := args.NamedChild(0)
	switch first.Kind() {
	case "string":
		return strings.Trim(first.Utf8Text(src), "'\"`"), true
	case "template_string":
		return strings.Trim(first.Utf8Text(src), "`"), true
	}
	return "", false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
