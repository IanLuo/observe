package analyze

import (
	"sort"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// decisionKinds are the node kinds that add a branch to a declaration. A
// declaration's cyclomatic complexity is one plus the branches it contains, so a
// straight-line function is 1 and every condition adds a path a reader must hold
// in mind.
var decisionKinds = map[string]map[string]bool{
	"go": {
		"if_statement":       true,
		"for_statement":      true,
		"expression_case":    true, // case in an expression switch
		"type_case":          true, // case in a type switch
		"communication_case": true, // case in a select
	},
	"typescript": {
		"if_statement":       true,
		"for_statement":      true,
		"for_in_statement":   true,
		"while_statement":    true,
		"do_statement":       true,
		"switch_case":        true,
		"catch_clause":       true,
		"ternary_expression": true,
	},
}

// shortCircuitOperators are the binary operators that add a path without a new
// block: "a && b" is two ways through the expression.
var shortCircuitOperators = map[string]bool{"&&": true, "||": true, "??": true}

// nestedFunctionKinds are the nodes whose branches belong to themselves. A handler
// written inline keeps its own decisions: counting them against the enclosing
// function said a 400-line server factory had complexity 108.
var nestedFunctionKinds = map[string]map[string]bool{
	"go": {
		"function_declaration": true,
		"method_declaration":   true,
		"func_literal":         true,
	},
	"typescript": {
		"function_declaration": true,
		"function_expression":  true,
		"arrow_function":       true,
		"method_definition":    true,
		"generator_function":   true,
	},
}

// functionKinds are the declarations whose branches belong to them. A class or a
// type has no branches of its own, and a handler registered inside a factory must
// not be counted against the factory: its branches are the handler's.
var functionKinds = map[string]map[string]bool{
	"go": {
		"function_declaration": true,
		"method_declaration":   true,
	},
	"typescript": {
		"function_declaration": true,
		"method_definition":    true,
	},
}

// DecisionCount is one construct that added a path, with the word a reader would
// use for it. The parts of a declaration add up to its complexity minus the one path
// every declaration has.
type DecisionCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// decisionLabels name the counted constructs. A reader re-deriving a number needs
// the word the code uses, not a grammar node name.
var decisionLabels = map[string]string{
	"if_statement":       "if",
	"for_statement":      "for",
	"for_in_statement":   "for…in",
	"while_statement":    "while",
	"do_statement":       "do",
	"switch_case":        "case",
	"expression_case":    "case",
	"type_case":          "case",
	"communication_case": "case",
	"catch_clause":       "catch",
	"ternary_expression": "?:",
	"&&":                 "&&",
	"||":                 "||",
	"??":                 "??",
}

// complexityOf counts the branches of one function: one plus the decision points in
// its own syntax, not in the functions it declares. The same code always yields the
// same number, so a reader can re-derive it (AC17).
//
// A declaration that is not a function scores zero rather than being counted as
// trivially simple: a class is not one path through the code.
func complexityOf(lang string, n *ts.Node, src []byte) (int, []DecisionCount) {
	if !functionKinds[lang][n.Kind()] {
		return 0, nil
	}

	kinds := decisionKinds[lang]
	nested := nestedFunctionKinds[lang]
	count := 1
	tally := map[string]int{}
	var walk func(node *ts.Node)
	walk = func(node *ts.Node) {
		if nested[node.Kind()] {
			return // a nested function's branches are its own
		}
		switch {
		case kinds[node.Kind()]:
			count++
			tally[node.Kind()]++
		case node.Kind() == "binary_expression":
			if op := node.ChildByFieldName("operator"); op != nil && shortCircuitOperators[op.Utf8Text(src)] {
				count++
				tally[op.Utf8Text(src)]++
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	// Start at the declaration's own children: the declaration node itself is not a
	// branch, and an "if" at the top of a function still is.
	for i := uint(0); i < n.NamedChildCount(); i++ {
		walk(n.NamedChild(i))
	}
	return count, breakdown(tally)
}

// breakdown turns the tally into a list, biggest part first, with the label a
// reader uses. Ties sort by label so the same code always prints the same way.
func breakdown(tally map[string]int) []DecisionCount {
	if len(tally) == 0 {
		return nil
	}
	out := make([]DecisionCount, 0, len(tally))
	for kind, count := range tally {
		label := decisionLabels[kind]
		if label == "" {
			label = kind
		}
		out = append(out, DecisionCount{Label: label, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}
