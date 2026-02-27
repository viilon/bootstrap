package dag

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// Resolve builds the dependency graph, checks for missing dependencies and cycles,
// and returns the nodes in topological order.
func Resolve(nodes []*Node) ([]*Node, error) {
	// 1. Map outputs to producers
	producers := make(map[reflect.Type]*Node)
	for _, n := range nodes {
		for _, out := range n.Outputs {
			if existing, ok := producers[out]; ok {
				return nil, fmt.Errorf("duplicate provider for type %v: %s and %s",
					out, nodeLabel(existing), nodeLabel(n))
			}
			producers[out] = n
		}
	}

	// 2. Build dependency graph
	deps := make(map[*Node][]*Node)
	for _, n := range nodes {
		for _, in := range n.Inputs {
			prod, ok := producers[in]
			if ok {
				deps[n] = append(deps[n], prod)
			} else {
				return nil, fmt.Errorf("missing dependency for type %v in %s", in, nodeLabel(n))
			}
		}
	}

	// 3. Topological Sort (includes cycle detection)
	return topologicalSort(nodes, deps)
}

func topologicalSort(nodes []*Node, deps map[*Node][]*Node) ([]*Node, error) {
	// First, check for cycles
	if err := checkCycles(nodes, deps); err != nil {
		return nil, err
	}

	// Then, perform topological sort
	var (
		visited = make(map[*Node]bool)
		sorted  = make([]*Node, 0)
		visit   func(*Node)
	)

	visit = func(n *Node) {
		if visited[n] {
			return
		}

		for _, m := range deps[n] {
			visit(m)
		}

		visited[n] = true
		sorted = append(sorted, n)
	}

	for _, n := range nodes {
		if !visited[n] {
			visit(n)
		}
	}

	return sorted, nil
}

func checkCycles(nodes []*Node, deps map[*Node][]*Node) error {
	var (
		visited = make(map[*Node]bool)
		inStack = make(map[*Node]int) // node -> index in stack
		stack   = make([]*Node, 0)    // current DFS path
		visit   func(*Node) error
	)

	visit = func(n *Node) error {
		if idx, ok := inStack[n]; ok {
			// Construct cycle path: stack[idx:] -> n
			cycleNodes := append(stack[idx:], n)
			parts := make([]string, 0, len(cycleNodes))
			for _, p := range cycleNodes {
				parts = append(parts, nodeLabel(p))
			}
			return errors.New("cyclic dependence: " + strings.Join(parts, " -> "))
		}
		if visited[n] {
			return nil
		}

		inStack[n] = len(stack)
		stack = append(stack, n)

		for _, m := range deps[n] {
			if err := visit(m); err != nil {
				return err
			}
		}

		// Pop stack
		stack = stack[:len(stack)-1]
		delete(inStack, n)

		visited[n] = true
		return nil
	}

	for _, n := range nodes {
		if !visited[n] {
			if err := visit(n); err != nil {
				return err
			}
		}
	}
	return nil
}

func nodeLabel(n *Node) string {
	pc := n.Fn.Pointer()
	f := runtime.FuncForPC(pc)
	if f == nil {
		return "unknown"
	}
	name := f.Name()
	// Anonymous functions usually contain .func suffix; show file line number in this case
	if strings.Contains(name, ".func") {
		file, line := f.FileLine(pc)
		return fmt.Sprintf("%s:%d", file, line)
	}
	return name
}
