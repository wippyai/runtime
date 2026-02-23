// SPDX-License-Identifier: MPL-2.0

package graph

import (
	"testing"

	"github.com/wippyai/runtime/internal/graph"
)

func TestOverlapDetector(t *testing.T) {
	t.Run("no overlaps with single request", func(t *testing.T) {
		detector := newOverlapDetector()
		nameA := MustParseName("wippy/actor")
		keyRoot := ModuleKey{}

		detector.recordRequest(nameA, "^1.0.0", keyRoot)

		overlaps := detector.detectOverlaps()
		if len(overlaps) != 0 {
			t.Errorf("expected no overlaps, got %d", len(overlaps))
		}
	})

	t.Run("detects overlap with multiple requests", func(t *testing.T) {
		detector := newOverlapDetector()
		nameA := MustParseName("wippy/actor")
		keyRoot := ModuleKey{}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		detector.recordRequest(nameA, "^1.0.0", keyRoot)
		detector.recordRequest(nameA, "^1.2.0", keyB)

		overlaps := detector.detectOverlaps()
		if len(overlaps) != 1 {
			t.Errorf("expected 1 overlap, got %d", len(overlaps))
		}

		if overlaps[0].Module != nameA {
			t.Errorf("expected overlap for %s, got %s", nameA, overlaps[0].Module)
		}

		if len(overlaps[0].Constraints) != 2 {
			t.Errorf("expected 2 constraints, got %d", len(overlaps[0].Constraints))
		}
	})

	t.Run("deduplicates same constraint from same requester", func(t *testing.T) {
		detector := newOverlapDetector()
		nameA := MustParseName("wippy/actor")
		keyRoot := ModuleKey{}

		detector.recordRequest(nameA, "^1.0.0", keyRoot)
		detector.recordRequest(nameA, "^1.0.0", keyRoot)

		overlaps := detector.detectOverlaps()
		if len(overlaps) != 0 {
			t.Errorf("expected no overlaps for duplicate requests, got %d", len(overlaps))
		}
	})

	t.Run("tracks multiple requesters for same constraint", func(t *testing.T) {
		detector := newOverlapDetector()
		nameA := MustParseName("wippy/actor")
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}
		keyC := ModuleKey{Name: MustParseName("wippy/c"), Version: "1.0.0"}

		detector.recordRequest(nameA, "^1.0.0", keyB)
		detector.recordRequest(nameA, "^1.0.0", keyC)

		// Should not be an overlap (same constraint)
		overlaps := detector.detectOverlaps()
		if len(overlaps) != 0 {
			t.Errorf("expected no overlaps for same constraint, got %d", len(overlaps))
		}
	})
}

func TestCheckCycles(t *testing.T) {
	t.Run("no cycle in simple graph", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})

		err := checkCycles(g)
		if err != nil {
			t.Errorf("expected no cycle, got error: %v", err)
		}
	})

	t.Run("detects direct cycle", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})
		g.AddEdge(keyB, keyA, 1, DependencyEdge{})

		err := checkCycles(g)
		if err == nil {
			t.Error("expected cycle error, got nil")
		}
	})

	t.Run("detects indirect cycle", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}
		keyC := ModuleKey{Name: MustParseName("wippy/c"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddNode(keyC)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})
		g.AddEdge(keyB, keyC, 1, DependencyEdge{})
		g.AddEdge(keyC, keyA, 1, DependencyEdge{})

		err := checkCycles(g)
		if err == nil {
			t.Error("expected cycle error, got nil")
		}
	})

	t.Run("detects self-dependency", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddEdge(keyA, keyA, 1, DependencyEdge{})

		err := checkCycles(g)
		if err == nil {
			t.Error("expected cycle error for self-dependency, got nil")
		}
	})
}

func TestDetectConflicts(t *testing.T) {
	t.Run("no conflicts with compatible constraints", func(t *testing.T) {
		overlaps := []constraintSet{
			{
				Module: MustParseName("wippy/actor"),
				Constraints: []ConstraintRequest{
					{Constraint: "^1.0.0", RequestedBy: []ModuleKey{{}}},
					{Constraint: "^1.2.0", RequestedBy: []ModuleKey{{}}},
				},
			},
		}

		g := graph.New[ModuleKey, DependencyEdge]()
		resolved := make(map[ModuleKey]ResolvedModule)
		keyA := ModuleKey{Name: MustParseName("wippy/actor"), Version: "1.3.0"}
		resolved[keyA] = ResolvedModule{Version: "1.3.0"}
		g.AddNode(keyA)

		conflicts := detectConflicts(overlaps, resolved, g)
		if len(conflicts) != 0 {
			t.Errorf("expected no conflicts, got %d", len(conflicts))
		}
	})

	t.Run("detects incompatible constraints", func(t *testing.T) {
		overlaps := []constraintSet{
			{
				Module: MustParseName("wippy/actor"),
				Constraints: []ConstraintRequest{
					{Constraint: "~1.2.0", RequestedBy: []ModuleKey{{}}},
					{Constraint: "~1.3.0", RequestedBy: []ModuleKey{{}}},
				},
			},
		}

		g := graph.New[ModuleKey, DependencyEdge]()
		resolved := make(map[ModuleKey]ResolvedModule)

		conflicts := detectConflicts(overlaps, resolved, g)
		if len(conflicts) == 0 {
			t.Error("expected conflict, got none")
		}

		if conflicts[0].Reason != ConflictIncompatibleConstraints {
			t.Errorf("expected ConflictIncompatibleConstraints, got %v", conflicts[0].Reason)
		}
	})

	t.Run("detects circular dependency", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})
		g.AddEdge(keyB, keyA, 1, DependencyEdge{})

		resolved := make(map[ModuleKey]ResolvedModule)
		var overlaps []constraintSet

		conflicts := detectConflicts(overlaps, resolved, g)
		if len(conflicts) == 0 {
			t.Error("expected conflict for circular dependency, got none")
		}

		if conflicts[0].Reason != ConflictCircularDependency {
			t.Errorf("expected ConflictCircularDependency, got %v", conflicts[0].Reason)
		}
	})
}

func TestFindPath(t *testing.T) {
	t.Run("finds direct path", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})

		path := findPath(g, keyA, keyB)
		if path == nil {
			t.Error("expected path, got nil")
		}
		if len(path) != 2 {
			t.Errorf("expected path length 2, got %d", len(path))
		}
	})

	t.Run("finds indirect path", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}
		keyC := ModuleKey{Name: MustParseName("wippy/c"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)
		g.AddNode(keyC)
		g.AddEdge(keyA, keyB, 1, DependencyEdge{})
		g.AddEdge(keyB, keyC, 1, DependencyEdge{})

		path := findPath(g, keyA, keyC)
		if path == nil {
			t.Error("expected path, got nil")
		}
		if len(path) != 3 {
			t.Errorf("expected path length 3, got %d", len(path))
		}
	})

	t.Run("returns nil for no path", func(t *testing.T) {
		g := graph.New[ModuleKey, DependencyEdge]()
		keyA := ModuleKey{Name: MustParseName("wippy/a"), Version: "1.0.0"}
		keyB := ModuleKey{Name: MustParseName("wippy/b"), Version: "1.0.0"}

		g.AddNode(keyA)
		g.AddNode(keyB)

		path := findPath(g, keyA, keyB)
		if path != nil {
			t.Errorf("expected nil path, got %v", path)
		}
	})
}
