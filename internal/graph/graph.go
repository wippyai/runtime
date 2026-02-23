// SPDX-License-Identifier: MPL-2.0

package graph

import (
	"container/heap"
	"fmt"
	"sort"
	"sync"
)

type Edge[T comparable, E any] struct {
	To     T
	Data   E
	Weight int
}

type Graph[T comparable, E any] struct {
	nodes map[T]bool
	edges map[T]map[T]Edge[T, E]
	mu    sync.RWMutex
}

func New[T comparable, E any]() *Graph[T, E] {
	return &Graph[T, E]{
		nodes: make(map[T]bool),
		edges: make(map[T]map[T]Edge[T, E]),
	}
}

func (g *Graph[T, E]) AddNode(n T) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n] = true
}

func (g *Graph[T, E]) AddEdge(from, to T, weight int, data E) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.edges[from]; !ok {
		g.edges[from] = make(map[T]Edge[T, E])
	}
	g.edges[from][to] = Edge[T, E]{
		To:     to,
		Weight: weight,
		Data:   data,
	}
}

func (g *Graph[T, E]) RemoveNode(n T) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.nodes[n] {
		return NewNodeDoesNotExistError(n)
	}

	delete(g.nodes, n)
	delete(g.edges, n)

	for from, edges := range g.edges {
		delete(edges, n)
		if len(edges) == 0 {
			delete(g.edges, from)
		}
	}

	return nil
}

func (g *Graph[T, E]) HasNode(n T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[n]
}

func (g *Graph[T, E]) HasEdge(from, to T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if edges, exists := g.edges[from]; exists {
		_, hasEdge := edges[to]
		return hasEdge
	}
	return false
}

func (g *Graph[T, E]) GetEdge(from, to T) (Edge[T, E], bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if edges, exists := g.edges[from]; exists {
		if edge, hasEdge := edges[to]; hasEdge {
			return edge, true
		}
	}
	return Edge[T, E]{}, false
}

func (g *Graph[T, E]) RemoveEdge(from, to T) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.nodes[from] {
		return NewNodeDoesNotExistError(from)
	}
	if !g.nodes[to] {
		return NewNodeDoesNotExistError(to)
	}

	edges, exists := g.edges[from]
	if !exists {
		return NewNoEdgesFromNodeError(from)
	}

	if _, hasEdge := edges[to]; !hasEdge {
		return NewEdgeDoesNotExistError(from, to)
	}

	delete(g.edges[from], to)

	if len(g.edges[from]) == 0 {
		delete(g.edges, from)
	}

	return nil
}

func (g *Graph[T, E]) GetNodes() []T {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]T, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

func (g *Graph[T, E]) Clone() *Graph[T, E] {
	g.mu.RLock()
	defer g.mu.RUnlock()

	cloned := New[T, E]()

	for node := range g.nodes {
		cloned.nodes[node] = true
	}

	for from, edges := range g.edges {
		cloned.edges[from] = make(map[T]Edge[T, E])
		for to, edge := range edges {
			cloned.edges[from][to] = Edge[T, E]{
				To:     edge.To,
				Weight: edge.Weight,
				Data:   edge.Data,
			}
		}
	}

	return cloned
}

func (g *Graph[T, E]) GetNeighbors(n T) ([]T, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.nodes[n] {
		return nil, NewNodeDoesNotExistError(n)
	}

	edges, exists := g.edges[n]
	if !exists {
		return []T{}, nil
	}

	neighbors := make([]T, 0, len(edges))
	for neighbor := range edges {
		neighbors = append(neighbors, neighbor)
	}
	return neighbors, nil
}

func (g *Graph[T, E]) findCycle() ([]T, bool) {
	visited := make(map[T]bool)
	recStack := make(map[T]bool)
	parent := make(map[T]T)

	var dfs func(T) ([]T, bool)
	dfs = func(node T) ([]T, bool) {
		visited[node] = true
		recStack[node] = true

		if edges, exists := g.edges[node]; exists {
			for neighbor := range edges {
				if !visited[neighbor] {
					parent[neighbor] = node
					if cycle, found := dfs(neighbor); found {
						return cycle, true
					}
				} else if recStack[neighbor] {
					cycle := []T{neighbor}
					current := node
					for current != neighbor {
						cycle = append([]T{current}, cycle...)
						if p, exists := parent[current]; exists {
							current = p
						} else {
							// Parent not found - return what we have for debugging
							cycle = append([]T{current}, cycle...)
							return cycle, true
						}
					}
					return cycle, true
				}
			}
		}

		recStack[node] = false
		return nil, false
	}

	for node := range g.nodes {
		if !visited[node] {
			if cycle, found := dfs(node); found {
				return cycle, true
			}
		}
	}

	return nil, false
}

func (g *Graph[T, E]) DependencyLevels() (*DependencyLevels[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	inDegree := make(map[T]int)
	for node := range g.nodes {
		inDegree[node] = 0
	}
	for _, edges := range g.edges {
		for to := range edges {
			inDegree[to]++
		}
	}

	result := &DependencyLevels[T]{
		levels: make([][]T, 0),
	}

	foundNodes := true
	for len(inDegree) > 0 && foundNodes {
		currentLevel := make([]T, 0)
		foundNodes = false

		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
				foundNodes = true
			}
		}

		if !foundNodes && len(inDegree) > 0 {
			if cycle, found := g.findCycle(); found {
				return nil, NewCycleDetectedError(cycle)
			}

			// Build detailed error with stuck nodes and their dependencies
			stuckNodes := make([]T, 0, len(inDegree))
			for node := range inDegree {
				stuckNodes = append(stuckNodes, node)
			}
			sort.Slice(stuckNodes, func(i, j int) bool {
				return fmt.Sprintf("%v", stuckNodes[i]) < fmt.Sprintf("%v", stuckNodes[j])
			})

			var depsInfo []string
			for _, node := range stuckNodes {
				deps := make([]T, 0)
				if edges, exists := g.edges[node]; exists {
					for dep := range edges {
						deps = append(deps, dep)
					}
				}
				sort.Slice(deps, func(i, j int) bool {
					return fmt.Sprintf("%v", deps[i]) < fmt.Sprintf("%v", deps[j])
				})
				depsInfo = append(depsInfo, fmt.Sprintf("%v (degree=%d, depends on: %v)", node, inDegree[node], deps))
			}

			return nil, NewCycleDetectedWithStuckNodesError(fmt.Sprintf("%v", depsInfo))
		}

		for _, node := range currentLevel {
			if edges, exists := g.edges[node]; exists {
				for neighbor := range edges {
					inDegree[neighbor]--
				}
			}
			delete(inDegree, node)
		}

		sort.Slice(currentLevel, func(i, j int) bool {
			return fmt.Sprintf("%v", currentLevel[i]) < fmt.Sprintf("%v", currentLevel[j])
		})

		if len(currentLevel) > 0 {
			result.levels = append(result.levels, currentLevel)
		}
	}

	return result, nil
}

func (g *Graph[T, E]) ShortestPath(from, to T) (*Path[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.HasNode(from) {
		return nil, NewNodeDoesNotExistError(from)
	}
	if !g.HasNode(to) {
		return nil, NewNodeDoesNotExistError(to)
	}

	distances := make(map[T]int)
	hops := make(map[T]int)
	previous := make(map[T]T)
	pq := &priorityQueue[T]{items: make([]*item[T], 0)}
	heap.Init(pq)

	for node := range g.nodes {
		if node == from {
			distances[node] = 0
			hops[node] = 0
			heap.Push(pq, &item[T]{node: node, priority: 0})
		} else {
			distances[node] = -1
			hops[node] = -1
		}
	}

	for pq.Len() > 0 {
		current := heap.Pop(pq).(*item[T])

		if current.priority > distances[current.node] {
			continue
		}

		neighbors := make([]T, 0, len(g.edges[current.node]))
		for neighbor := range g.edges[current.node] {
			neighbors = append(neighbors, neighbor)
		}
		sort.Slice(neighbors, func(i, j int) bool {
			return fmt.Sprintf("%v", neighbors[i]) < fmt.Sprintf("%v", neighbors[j])
		})

		for _, neighbor := range neighbors {
			edge := g.edges[current.node][neighbor]
			newDist := distances[current.node] + edge.Weight
			newHops := hops[current.node] + 1

			update := false
			switch {
			case distances[neighbor] == -1:
				update = true
			case newDist < distances[neighbor]:
				update = true
			case newDist == distances[neighbor] && (hops[neighbor] == -1 || newHops < hops[neighbor]):
				update = true
			case newDist == distances[neighbor] && newHops == hops[neighbor]:
				prev, ok := previous[neighbor]
				update = !ok || fmt.Sprintf("%v", current.node) < fmt.Sprintf("%v", prev)
			}

			if update {
				distances[neighbor] = newDist
				hops[neighbor] = newHops
				previous[neighbor] = current.node
				heap.Push(pq, &item[T]{
					node:     neighbor,
					priority: newDist,
				})
			}
		}
	}

	if distances[to] == -1 {
		return nil, NewNoPathExistsError(from, to)
	}

	path := &Path[T]{
		Cost:  distances[to],
		Nodes: make([]T, 0),
	}

	for current := to; ; current = previous[current] {
		path.Nodes = append([]T{current}, path.Nodes...)
		if current == from {
			break
		}
	}

	return path, nil
}
