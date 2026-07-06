// SPDX-License-Identifier: MPL-2.0

// Package plan computes deterministic execution plans: diff/classify, the
// operation graph, cycle rejection, and Kahn waves
// (docs/design/cloud-graph.md §8). Pure functions; no gonum types escape.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wippyai/runtime/service/cloudgraph/resource"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// OpEntry is one planned operation inside the stored plan document.
type OpEntry struct {
	OpID       string          `json:"op_id"`
	ResourceID string          `json:"resource_id"`
	Action     resource.Action `json:"action"`
}

// EdgeEntry is one hard dependency edge inside the stored plan document.
type EdgeEntry struct {
	Source string                  `json:"source"`
	Target string                  `json:"target"`
	Kind   resource.DependencyKind `json:"kind"`
}

// Document is the persisted plan artifact. All collections are ordered
// arrays so the workflow never iterates a map (design §8.3).
type Document struct {
	PlanID     string      `json:"plan_id"`
	DeployID   string      `json:"deploy_id"`
	SpecHash   string      `json:"spec_hash"`
	Operations []OpEntry   `json:"operations"`
	Edges      []EdgeEntry `json:"edges"`
	Waves      [][]string  `json:"waves"`
}

// Compute builds the plan document for a deploy. Identical inputs produce a
// byte-identical document (design §8.3). The PoC classifies every spec as
// create; soft edges are recorded but never scheduled (design §8.2).
func Compute(deployID string, specs []resource.Spec) (*Document, error) {
	if len(specs) == 0 {
		return nil, NewEmptyDeployError(deployID)
	}

	sorted := make([]resource.Spec, len(specs))
	copy(sorted, specs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ResourceID < sorted[j].ResourceID })

	index := make(map[string]int64, len(sorted))
	for i, spec := range sorted {
		if spec.ResourceID == "" {
			return nil, NewInvalidSpecError("resource_id is required")
		}
		if _, dup := index[spec.ResourceID]; dup {
			return nil, NewInvalidSpecError("duplicate resource_id: " + spec.ResourceID)
		}
		index[spec.ResourceID] = int64(i)
	}

	edges, err := collectEdges(sorted, index)
	if err != nil {
		return nil, err
	}

	waves, err := computeWaves(sorted, index, edges)
	if err != nil {
		return nil, err
	}

	ops := make([]OpEntry, len(sorted))
	for i, spec := range sorted {
		ops[i] = OpEntry{
			OpID:       OperationID(deployID, spec.ResourceID),
			ResourceID: spec.ResourceID,
			Action:     resource.ActionCreate,
		}
	}

	waveIDs := make([][]string, len(waves))
	for w, wave := range waves {
		waveIDs[w] = make([]string, len(wave))
		for i, nodeID := range wave {
			waveIDs[w][i] = OperationID(deployID, sorted[nodeID].ResourceID)
		}
	}

	hash, err := hashSortedSpecs(sorted)
	if err != nil {
		return nil, err
	}

	return &Document{
		PlanID:     "plan/" + deployID + "/" + hash[:12],
		DeployID:   deployID,
		SpecHash:   hash,
		Operations: ops,
		Edges:      edges,
		Waves:      waveIDs,
	}, nil
}

// OperationID derives the deterministic operation identifier of a resource
// within a deploy.
func OperationID(deployID, resourceID string) string {
	return deployID + "/" + resourceID
}

// collectEdges merges declared dependencies with $ref-derived configure edges,
// validated and deduplicated, ordered deterministically.
func collectEdges(sorted []resource.Spec, index map[string]int64) ([]EdgeEntry, error) {
	seen := make(map[EdgeEntry]struct{})
	var edges []EdgeEntry

	add := func(edge EdgeEntry) {
		if _, dup := seen[edge]; dup {
			return
		}
		seen[edge] = struct{}{}
		edges = append(edges, edge)
	}

	for _, spec := range sorted {
		names := make([]string, 0, len(spec.Dependencies))
		for name := range spec.Dependencies {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			dep := spec.Dependencies[name]
			if !dep.Kind.Valid() {
				return nil, NewInvalidSpecError(fmt.Sprintf("%s: dependency %q has invalid kind %q", spec.ResourceID, name, dep.Kind))
			}
			if dep.Target == spec.ResourceID {
				return nil, NewInvalidSpecError(spec.ResourceID + ": dependency on itself")
			}
			if _, ok := index[dep.Target]; !ok {
				return nil, NewInvalidSpecError(fmt.Sprintf("%s: dependency %q targets unknown resource %q", spec.ResourceID, name, dep.Target))
			}
			add(EdgeEntry{Source: spec.ResourceID, Target: dep.Target, Kind: dep.Kind})
		}

		refs, err := Refs(spec.Desired)
		if err != nil {
			return nil, NewInvalidSpecError(spec.ResourceID + ": " + err.Error())
		}
		for _, ref := range refs {
			if ref.ResourceID == spec.ResourceID {
				return nil, NewInvalidSpecError(spec.ResourceID + ": $ref to itself")
			}
			if _, ok := index[ref.ResourceID]; !ok {
				return nil, NewInvalidSpecError(fmt.Sprintf("%s: $ref targets unknown resource %q", spec.ResourceID, ref.ResourceID))
			}
			add(EdgeEntry{Source: spec.ResourceID, Target: ref.ResourceID, Kind: resource.DepConfigure})
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Kind < edges[j].Kind
	})
	return edges, nil
}

// computeWaves builds the hard-edge scheduling graph, rejects cycles, and
// returns Kahn waves of node indices in ascending order. gonum iterator order
// is never relied on (design §8.2).
func computeWaves(sorted []resource.Spec, index map[string]int64, edges []EdgeEntry) ([][]int64, error) {
	g := simple.NewDirectedGraph()
	for i := range sorted {
		g.AddNode(simple.Node(int64(i)))
	}

	type pair struct{ from, to int64 }
	hard := make(map[pair]struct{})
	for _, edge := range edges {
		if edge.Kind == resource.DepSoft {
			continue
		}
		p := pair{from: index[edge.Target], to: index[edge.Source]}
		if _, dup := hard[p]; dup {
			continue
		}
		hard[p] = struct{}{}
		g.SetEdge(g.NewEdge(simple.Node(p.from), simple.Node(p.to)))
	}

	if _, err := topo.Sort(g); err != nil {
		return nil, cycleError(sorted, g)
	}

	n := len(sorted)
	indegree := make([]int, n)
	successors := make([][]int64, n)
	for p := range hard {
		indegree[p.to]++
		successors[p.from] = append(successors[p.from], p.to)
	}
	for i := range successors {
		sort.Slice(successors[i], func(a, b int) bool { return successors[i][a] < successors[i][b] })
	}

	var waves [][]int64
	var ready []int64
	for i := range n {
		if indegree[i] == 0 {
			ready = append(ready, int64(i))
		}
	}

	processed := 0
	for len(ready) > 0 {
		sort.Slice(ready, func(a, b int) bool { return ready[a] < ready[b] })
		waves = append(waves, ready)
		processed += len(ready)

		var next []int64
		for _, id := range ready {
			for _, succ := range successors[id] {
				indegree[succ]--
				if indegree[succ] == 0 {
					next = append(next, succ)
				}
			}
		}
		ready = next
	}

	if processed != n {
		return nil, cycleError(sorted, g)
	}
	return waves, nil
}

// cycleError names every strongly connected component with more than one
// member, deterministically sorted (design §8.2).
func cycleError(sorted []resource.Spec, g *simple.DirectedGraph) error {
	var cycles []string
	for _, scc := range topo.TarjanSCC(g) {
		if len(scc) < 2 {
			continue
		}
		members := make([]string, len(scc))
		for i, node := range scc {
			members[i] = sorted[node.ID()].ResourceID
		}
		sort.Strings(members)
		cycles = append(cycles, strings.Join(members, " -> "))
	}
	sort.Strings(cycles)
	return NewDependencyCycleError(strings.Join(cycles, "; "))
}

// SpecHash is the SHA-256 over the canonically ordered spec set; identical
// declarations hash identically regardless of submission order.
func SpecHash(specs []resource.Spec) (string, error) {
	sorted := make([]resource.Spec, len(specs))
	copy(sorted, specs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ResourceID < sorted[j].ResourceID })
	return hashSortedSpecs(sorted)
}

func hashSortedSpecs(sorted []resource.Spec) (string, error) {
	canonical, err := json.Marshal(sorted)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
