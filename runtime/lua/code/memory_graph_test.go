package lua

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// createTestNode creates a test node with the given ID.
func createTestNode(id string) *runtime.Node {
	return &runtime.Node{
		ID:     registry.ID{Name: id},
		Kind:   "function.lua",
		Source: fmt.Sprintf("function %s() return 'hello' end", id),
		Method: "test",
	}
}

// dummyModule complies with the runtime.Module interface.
type dummyModule struct {
	name string
}

// Loader is a dummy loader implementation.
func (d *dummyModule) Loader(L *lua.LState) int {
	return 0
}

// Name returns the dummy module's name.
func (d *dummyModule) Name() string {
	return d.name
}

// TestMemoryGraph_AddNode tests node addition, including duplicate and nil cases.
func TestMemoryGraph_AddNode(t *testing.T) {
	mg := NewMemoryGraph()

	t.Run("ValidAddition", func(t *testing.T) {
		node := createTestNode("node1")
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		got, err := mg.GetNode(node.ID)
		if err != nil {
			t.Fatalf("failed to retrieve node: %v", err)
		}
		if !reflect.DeepEqual(got, node) {
			t.Errorf("retrieved node does not match added node.\nGot: %+v\nWant: %+v", got, node)
		}
	})

	t.Run("DuplicateAddition", func(t *testing.T) {
		node := createTestNode("nodeDup")
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := mg.AddNode(node); err == nil {
			t.Errorf("expected error on duplicate addition, got nil")
		}
	})

	t.Run("NilAddition", func(t *testing.T) {
		if err := mg.AddNode(nil); err == nil {
			t.Errorf("expected error when adding nil node, got nil")
		}
	})
}

// TestMemoryGraph_RemoveNode tests node removal.
func TestMemoryGraph_RemoveNode(t *testing.T) {
	mg := NewMemoryGraph()
	node := createTestNode("nodeToRemove")
	if err := mg.AddNode(node); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	t.Run("ValidRemoval", func(t *testing.T) {
		if err := mg.RemoveNode(node.ID); err != nil {
			t.Fatalf("failed to remove node: %v", err)
		}
		if _, err := mg.GetNode(node.ID); err == nil {
			t.Errorf("expected error retrieving removed node, got nil")
		}
	})

	t.Run("NonExistentRemoval", func(t *testing.T) {
		err := mg.RemoveNode(registry.ID{Name: "non-existent"})
		if err == nil {
			t.Errorf("expected error when removing non-existent node, got nil")
		}
	})
}

// TestMemoryGraph_AddDependency tests adding dependencies and cycle detection.
func TestMemoryGraph_AddDependency(t *testing.T) {
	mg := NewMemoryGraph()
	nodeA := createTestNode("A")
	nodeB := createTestNode("B")
	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add nodeA: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add nodeB: %v", err)
	}

	t.Run("ValidDependency", func(t *testing.T) {
		if err := mg.AddDependency(nodeA.ID, nodeB.ID, "alias1"); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		deps, err := mg.GetDirectDependencies(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to get dependencies: %v", err)
		}
		if len(deps) != 1 || deps[0].ID != nodeB.ID {
			t.Errorf("expected dependency to nodeB, got: %+v", deps)
		}
	})

	t.Run("MissingFromNode", func(t *testing.T) {
		err := mg.AddDependency(registry.ID{Name: "NonExistent"}, nodeB.ID, "alias")
		if err == nil {
			t.Errorf("expected error when 'from' node is missing, got nil")
		}
	})

	t.Run("MissingToNode", func(t *testing.T) {
		err := mg.AddDependency(nodeA.ID, registry.ID{Name: "NonExistent"}, "alias")
		if err == nil {
			t.Errorf("expected error when 'to' node is missing, got nil")
		}
	})

	t.Run("DuplicateDependency", func(t *testing.T) {
		// Dependency A -> B already added above.
		err := mg.AddDependency(nodeA.ID, nodeB.ID, "alias1")
		if err == nil {
			t.Errorf("expected error when adding duplicate dependency, got nil")
		}
	})

	t.Run("CycleDetection", func(t *testing.T) {
		// Adding B -> A should create a cycle (A -> B exists).
		err := mg.AddDependency(nodeB.ID, nodeA.ID, "cycleAlias")
		if err == nil {
			t.Errorf("expected cycle detection error, got nil")
		}
	})
}

// TestMemoryGraph_RemoveDependency tests dependency removal.
func TestMemoryGraph_RemoveDependency(t *testing.T) {
	mg := NewMemoryGraph()
	nodeA := createTestNode("DepA")
	nodeB := createTestNode("DepB")
	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add nodeA: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add nodeB: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "aliasDep"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	t.Run("ValidRemoval", func(t *testing.T) {
		if err := mg.RemoveDependency(nodeA.ID, nodeB.ID); err != nil {
			t.Fatalf("failed to remove dependency: %v", err)
		}
		deps, err := mg.GetDirectDependencies(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to get dependencies: %v", err)
		}
		if len(deps) != 0 {
			t.Errorf("expected no dependencies after removal, got: %+v", deps)
		}
	})

	t.Run("NonExistentRemoval", func(t *testing.T) {
		err := mg.RemoveDependency(nodeA.ID, nodeB.ID)
		if err == nil {
			t.Errorf("expected error when removing non-existent dependency, got nil")
		}
	})
}

// TestMemoryGraph_GetDirectDependencies tests retrieval of a node's outgoing edges.
func TestMemoryGraph_GetDirectDependencies(t *testing.T) {
	mg := NewMemoryGraph()
	nodeA := createTestNode("DepTestA")
	nodeB := createTestNode("DepTestB")
	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add nodeA: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add nodeB: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "depAlias"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	t.Run("ExistingDependencies", func(t *testing.T) {
		deps, err := mg.GetDirectDependencies(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to get dependencies: %v", err)
		}
		if len(deps) != 1 || deps[0].ID != nodeB.ID {
			t.Errorf("expected dependency to nodeB, got: %+v", deps)
		}
	})

	t.Run("NoDependencies", func(t *testing.T) {
		deps, err := mg.GetDirectDependencies(nodeB.ID)
		if err != nil {
			t.Fatalf("failed to get dependencies: %v", err)
		}
		if len(deps) != 0 {
			t.Errorf("expected no dependencies, got: %+v", deps)
		}
	})

	t.Run("NonExistentNode", func(t *testing.T) {
		_, err := mg.GetDirectDependencies(registry.ID{Name: "NonExistent"})
		if err == nil {
			t.Errorf("expected error for non-existent node, got nil")
		}
	})
}

// TestMemoryGraph_GetDirectDependents tests retrieval of a node's incoming edges.
func TestMemoryGraph_GetDirectDependents(t *testing.T) {
	mg := NewMemoryGraph()
	nodeA := createTestNode("DepdA")
	nodeB := createTestNode("DepdB")
	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add nodeA: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add nodeB: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "aliasForDepd"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	t.Run("ExistingDependents", func(t *testing.T) {
		deps, err := mg.GetDirectDependents(nodeB.ID)
		if err != nil {
			t.Fatalf("failed to get dependents: %v", err)
		}
		if len(deps) != 1 || deps[0].ID != nodeA.ID {
			t.Errorf("expected dependent to be nodeA, got: %+v", deps)
		}
	})

	t.Run("NoDependents", func(t *testing.T) {
		deps, err := mg.GetDirectDependents(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to get dependents: %v", err)
		}
		if len(deps) != 0 {
			t.Errorf("expected no dependents, got: %+v", deps)
		}
	})

	t.Run("NonExistentNode", func(t *testing.T) {
		_, err := mg.GetDirectDependents(registry.ID{Name: "NonExistent"})
		if err == nil {
			t.Errorf("expected error for non-existent node, got nil")
		}
	})
}

// TestMemoryGraph_DependencyLevels tests topological ordering and cycle detection.
func TestMemoryGraph_DependencyLevels(t *testing.T) {
	t.Run("AcyclicGraph", func(t *testing.T) {
		mg := NewMemoryGraph()
		nodeA := createTestNode("TopoA")
		nodeB := createTestNode("TopoB")
		nodeC := createTestNode("TopoC")
		if err := mg.AddNode(nodeA); err != nil {
			t.Fatalf("failed to add nodeA: %v", err)
		}
		if err := mg.AddNode(nodeB); err != nil {
			t.Fatalf("failed to add nodeB: %v", err)
		}
		if err := mg.AddNode(nodeC); err != nil {
			t.Fatalf("failed to add nodeC: %v", err)
		}
		if err := mg.AddDependency(nodeA.ID, nodeB.ID, ""); err != nil {
			t.Fatalf("failed to add dependency A->B: %v", err)
		}
		if err := mg.AddDependency(nodeB.ID, nodeC.ID, ""); err != nil {
			t.Fatalf("failed to add dependency B->C: %v", err)
		}

		levels, err := mg.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error in DependencyLevels: %v", err)
		}
		if len(levels) != 3 {
			t.Errorf("expected 3 levels, got %d", len(levels))
		}
		// Level 0 should contain TopoA, level 1 TopoB, level 2 TopoC.
		if len(levels[0]) != 1 || levels[0][0].ID.Name != "TopoA" {
			t.Errorf("expected level 0 to contain TopoA, got: %+v", levels[0])
		}
		if len(levels[1]) != 1 || levels[1][0].ID.Name != "TopoB" {
			t.Errorf("expected level 1 to contain TopoB, got: %+v", levels[1])
		}
		if len(levels[2]) != 1 || levels[2][0].ID.Name != "TopoC" {
			t.Errorf("expected level 2 to contain TopoC, got: %+v", levels[2])
		}
	})

	t.Run("CycleDetection", func(t *testing.T) {
		mg := NewMemoryGraph()
		nodeA := createTestNode("CycleA")
		nodeB := createTestNode("CycleB")
		nodeC := createTestNode("CycleC")
		if err := mg.AddNode(nodeA); err != nil {
			t.Fatalf("failed to add nodeA: %v", err)
		}
		if err := mg.AddNode(nodeB); err != nil {
			t.Fatalf("failed to add nodeB: %v", err)
		}
		if err := mg.AddNode(nodeC); err != nil {
			t.Fatalf("failed to add nodeC: %v", err)
		}
		if err := mg.AddDependency(nodeA.ID, nodeB.ID, ""); err != nil {
			t.Fatalf("failed to add dependency A->B: %v", err)
		}
		if err := mg.AddDependency(nodeB.ID, nodeC.ID, ""); err != nil {
			t.Fatalf("failed to add dependency B->C: %v", err)
		}
		// Introducing a cycle: CycleC -> CycleA.
		if err := mg.AddDependency(nodeC.ID, nodeA.ID, ""); err == nil {
			t.Errorf("expected cycle detection error, got nil")
		}
	})
}

// TestMemoryGraph_BuildRuntime tests construction of a Runtime configuration.
func TestMemoryGraph_BuildRuntime(t *testing.T) {
	mg := NewMemoryGraph()
	nodeA := createTestNode("MainNode")
	nodeB := createTestNode("DepNodeB")
	nodeC := createTestNode("DepNodeC")

	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add nodeA: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add nodeB: %v", err)
	}
	if err := mg.AddNode(nodeC); err != nil {
		t.Fatalf("failed to add nodeC: %v", err)
	}

	// Add dependencies from MainNode to DepNodeB and DepNodeC.
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "depB"); err != nil {
		t.Fatalf("failed to add dependency A->B: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeC.ID, ""); err != nil {
		t.Fatalf("failed to add dependency A->C: %v", err)
	}

	// Assign a dummy module to nodeC.
	var mod runtime.Module = &dummyModule{name: "dummyMod"}
	nodeC.Module = &mod

	t.Run("ValidRuntime", func(t *testing.T) {
		rt, err := mg.BuildRuntime(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to build runtime: %v", err)
		}
		// Verify main node.
		expectedMainAlias := fmt.Sprintf("entry_%v", nodeA.ID)
		if rt.Main.Node.ID != nodeA.ID || rt.Main.Alias != expectedMainAlias {
			t.Errorf("unexpected main node or alias, got: %+v", rt.Main)
		}
		// Check dependency prototypes (should include nodeB and nodeC).
		if len(rt.DepProtos) != 2 {
			t.Errorf("expected 2 dependency prototypes, got %d", len(rt.DepProtos))
		}
		// Verify alias propagation for nodeB.
		for _, dep := range rt.DepProtos {
			if dep.Node.ID == nodeB.ID && dep.Alias != "depB" {
				t.Errorf("expected alias 'depB' for nodeB, got '%s'", dep.Alias)
			}
		}
		// Check that modules include the one from nodeC.
		if len(rt.Modules) != 1 {
			t.Errorf("expected 1 module, got %d", len(rt.Modules))
		} else if (*rt.Modules[0]).Name() != "dummyMod" {
			t.Errorf("expected module name 'dummyMod', got '%s'", (*rt.Modules[0]).Name())
		}
	})

	t.Run("InvalidEntrypoint", func(t *testing.T) {
		_, err := mg.BuildRuntime(registry.ID{Name: "NonExistent"})
		if err == nil {
			t.Errorf("expected error for non-existent entrypoint, got nil")
		}
	})
}
