// Package engine — DAG topological sort + execution-stage planner.
//
// 1:1 port of the Rust crate's `execution_order`, `execution_stages`,
// `reachable_nodes`, `topological_sort` helpers. Kahn's algorithm
// drives the sort; cycle detection compares visited count against
// reachable count and surfaces the canonical
// `cycle detected in pipeline DAG` error.
package engine

import (
	"errors"
	"fmt"
	"sort"
)

// executionOrder mirrors `fn execution_order`. Filters the
// topologically-sorted node list to only the nodes reachable from
// `startFrom` when set.
func executionOrder(nodes []PipelineNode, startFrom *string) ([]string, error) {
	order, err := topologicalSort(nodes)
	if err != nil {
		return nil, err
	}
	if startFrom == nil {
		return order, nil
	}
	found := false
	for _, n := range nodes {
		if n.ID == *startFrom {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("start node '%s' not found", *startFrom)
	}
	reachable := reachableNodes(nodes, *startFrom)
	out := []string{}
	for _, id := range order {
		if _, ok := reachable[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// reachableNodes mirrors `fn reachable_nodes`. Returns the set of
// node ids transitively dependent on `start`.
func reachableNodes(nodes []PipelineNode, start string) map[string]struct{} {
	adjacency := map[string][]string{}
	for _, n := range nodes {
		if _, ok := adjacency[n.ID]; !ok {
			adjacency[n.ID] = nil
		}
		for _, dep := range n.DependsOn {
			adjacency[dep] = append(adjacency[dep], n.ID)
		}
	}
	reachable := map[string]struct{}{}
	stack := []string{start}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := reachable[id]; seen {
			continue
		}
		reachable[id] = struct{}{}
		for _, neighbor := range adjacency[id] {
			stack = append(stack, neighbor)
		}
	}
	return reachable
}

// topologicalSort mirrors `fn topological_sort` (Kahn's algorithm).
// Returns the canonical "cycle detected" error when the visited
// count doesn't match the input length.
func topologicalSort(nodes []PipelineNode) ([]string, error) {
	inDegree := map[string]int{}
	adjacency := map[string][]string{}
	for _, n := range nodes {
		if _, ok := inDegree[n.ID]; !ok {
			inDegree[n.ID] = 0
		}
		if _, ok := adjacency[n.ID]; !ok {
			adjacency[n.ID] = nil
		}
		for _, dep := range n.DependsOn {
			adjacency[dep] = append(adjacency[dep], n.ID)
			inDegree[n.ID]++
		}
	}
	queue := []string{}
	for id, d := range inDegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	// Sort the initial frontier so the order is stable across runs
	// (matches the Rust implementation that sorts when a deterministic
	// tie-break is needed).
	sort.Strings(queue)
	order := []string{}
	for len(queue) > 0 {
		// Rust pops from the end — preserve that order so any caller
		// that depends on the canonical sequence keeps round-tripping.
		n := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		order = append(order, n)
		for _, neighbor := range adjacency[n] {
			d := inDegree[neighbor] - 1
			inDegree[neighbor] = d
			if d == 0 {
				queue = append(queue, neighbor)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, errors.New("cycle detected in pipeline DAG")
	}
	return order, nil
}

// executionStages mirrors `fn execution_stages` from
// `dag_executor.rs`. Returns a Vec<Vec<String>> where each inner
// slice is one wave of independently-executable nodes (used by the
// distributed-worker scheduler).
func executionStages(nodes []PipelineNode, startFrom *string) ([][]string, error) {
	var reachable map[string]struct{}
	if startFrom != nil {
		found := false
		for _, n := range nodes {
			if n.ID == *startFrom {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("start node '%s' not found", *startFrom)
		}
		reachable = reachableNodes(nodes, *startFrom)
	} else {
		reachable = map[string]struct{}{}
		for _, n := range nodes {
			reachable[n.ID] = struct{}{}
		}
	}

	inDegree := map[string]int{}
	adjacency := map[string][]string{}
	for _, n := range nodes {
		if _, ok := reachable[n.ID]; !ok {
			continue
		}
		if _, ok := inDegree[n.ID]; !ok {
			inDegree[n.ID] = 0
		}
		if _, ok := adjacency[n.ID]; !ok {
			adjacency[n.ID] = nil
		}
		for _, dep := range n.DependsOn {
			if _, ok := reachable[dep]; !ok {
				continue
			}
			adjacency[dep] = append(adjacency[dep], n.ID)
			inDegree[n.ID]++
		}
	}

	frontier := []string{}
	for id, d := range inDegree {
		if d == 0 {
			frontier = append(frontier, id)
		}
	}
	sort.Strings(frontier)

	stages := [][]string{}
	visited := 0
	for len(frontier) > 0 {
		next := []string{}
		stage := make([]string, 0, len(frontier))
		for _, id := range frontier {
			visited++
			stage = append(stage, id)
			for _, neighbor := range adjacency[id] {
				d := inDegree[neighbor] - 1
				inDegree[neighbor] = d
				if d == 0 {
					next = append(next, neighbor)
				}
			}
		}
		sort.Strings(next)
		stages = append(stages, stage)
		frontier = next
	}
	if visited != len(reachable) {
		return nil, errors.New("cycle detected in pipeline DAG")
	}
	return stages, nil
}
