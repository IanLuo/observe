package analyze

import "sort"

// coupleModules fills in fan-in, fan-out, and instability for every module.
//
// Fan-out is how many modules this one imports; fan-in is how many import it.
// Instability is fan-out over total coupling: a module that imports a lot and is
// imported by nothing is unstable, which is a fact about the code, not a verdict
// (AC16).
func coupleModules(modules []Module, edges []ModuleEdge) {
	index := make(map[string]int, len(modules))
	for i, m := range modules {
		index[m.Dir] = i
	}
	for _, e := range edges {
		from, okFrom := index[e.From]
		to, okTo := index[e.To]
		if !okFrom || !okTo {
			continue
		}
		modules[from].FanOut++
		modules[to].FanIn++
	}
	for i := range modules {
		total := modules[i].FanIn + modules[i].FanOut
		if total == 0 {
			modules[i].Instability = 0
			continue
		}
		modules[i].Instability = float64(modules[i].FanOut) / float64(total)
	}
}

// findCycles returns the module cycles, each as its member directories in sorted
// order. It uses Tarjan's strongly connected components: a component of one module
// is a cycle only if that module imports itself, which the module graph hides, so
// singletons are dropped.
func findCycles(modules []Module, edges []ModuleEdge) [][]string {
	adjacency := make(map[string][]string, len(modules))
	for _, m := range modules {
		adjacency[m.Dir] = nil
	}
	for _, e := range edges {
		if _, ok := adjacency[e.From]; !ok {
			continue
		}
		if _, ok := adjacency[e.To]; !ok {
			continue
		}
		adjacency[e.From] = append(adjacency[e.From], e.To)
	}
	for from := range adjacency {
		sort.Strings(adjacency[from])
	}

	dirs := make([]string, 0, len(adjacency))
	for dir := range adjacency {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var (
		next    int
		index   = map[string]int{}
		low     = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		cycles  [][]string
	)

	var connect func(v string)
	connect = func(v string) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adjacency[v] {
			if _, seen := index[w]; !seen {
				connect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}

		if low[v] == index[v] {
			var component []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == v {
					break
				}
			}
			if len(component) > 1 {
				sort.Strings(component)
				cycles = append(cycles, component)
			}
		}
	}

	for _, dir := range dirs {
		if _, seen := index[dir]; !seen {
			connect(dir)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return cycles[i][0] < cycles[j][0] })
	return cycles
}
