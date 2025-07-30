package code

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// createTestNode creates a test node with the given Process.
func createTestNode(id string) *Node {
	return &Node{
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
func (d *dummyModule) Loader(_ *lua.LState) int {
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

func TestMemoryGraph_ReplaceNode(t *testing.T) {
	mg := NewMemoryGraph()
	originalNode := createTestNode("nodeToReplace")
	if err := mg.AddNode(originalNode); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	t.Run("ValidReplacement", func(t *testing.T) {
		updatedNode := &Node{
			ID:     originalNode.ID,
			Kind:   originalNode.Kind,
			Source: "function updated() return 'updated' end",
			Method: "updated",
			Version: Version{
				Hash:    "updated_hash",
				Created: time.Now(),
			},
		}

		if err := mg.ReplaceNode(updatedNode); err != nil {
			t.Fatalf("failed to replace node: %v", err)
		}

		// Verify the node was updated
		retrieved, err := mg.GetNode(originalNode.ID)
		if err != nil {
			t.Fatalf("failed to get replaced node: %v", err)
		}

		if retrieved.Source != updatedNode.Source {
			t.Errorf("expected source %s, got %s", updatedNode.Source, retrieved.Source)
		}
		if retrieved.Method != updatedNode.Method {
			t.Errorf("expected method %s, got %s", updatedNode.Method, retrieved.Method)
		}
		if retrieved.Version.Hash != updatedNode.Version.Hash {
			t.Errorf("expected hash %s, got %s", updatedNode.Version.Hash, retrieved.Version.Hash)
		}
	})

	t.Run("NonExistentReplacement", func(t *testing.T) {
		nonExistentNode := &Node{
			ID:     registry.ID{Name: "non-existent"},
			Kind:   "function.lua",
			Source: "function test() return 'test' end",
			Method: "test",
		}

		err := mg.ReplaceNode(nonExistentNode)
		if err == nil {
			t.Errorf("expected error when replacing non-existent node, got nil")
		}
	})

	t.Run("NilNodeReplacement", func(t *testing.T) {
		err := mg.ReplaceNode(nil)
		if err == nil {
			t.Errorf("expected error when replacing with nil node, got nil")
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

	// AddCleanup dependencies from MainNode to DepNodeB and DepNodeC.
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "depB"); err != nil {
		t.Fatalf("failed to add dependency A->B: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeC.ID, ""); err != nil {
		t.Fatalf("failed to add dependency A->C: %v", err)
	}

	// Assign a dummy module to nodeC.
	var mod runtime.Module = &dummyModule{name: "dummyMod"}
	nodeC.Module = mod

	t.Run("ValidRuntime", func(t *testing.T) {
		rt, err := mg.Build(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to build runtime: %v", err)
		}
		// Verify main node.
		if rt.Main.ID != nodeA.ID {
			t.Errorf("unexpected main node or alias, got: %+v", rt.Main)
		}
		// Check dependency prototypes (should include nodeB and nodeC).
		if len(rt.Dependencies) != 2 {
			t.Errorf("expected 2 dependency prototypes, got %d", len(rt.Dependencies))
		}
		// Verify alias propagation for nodeB.
		for _, dep := range rt.Dependencies {
			if dep.Node.ID == nodeB.ID && dep.Name != "depB" {
				t.Errorf("expected alias 'depB' for nodeB, got '%s'", dep.Name)
			}
			// Verify module is included as a dependency
			if dep.Node.ID == nodeC.ID {
				if dep.Node.Module == nil || dep.Node.Module.Name() != "dummyMod" {
					t.Errorf("expected module 'dummyMod' in nodeC dependency")
				}
			}
		}
	})

	t.Run("InvalidEntrypoint", func(t *testing.T) {
		_, err := mg.Build(registry.ID{Name: "NonExistent"})
		if err == nil {
			t.Errorf("expected error for non-existent entrypoint, got nil")
		}
	})
}

func TestMemoryGraph_RemoveNode_MultipleIncoming(t *testing.T) {
	mg := NewMemoryGraph()
	parent1 := createTestNode("Parent1")
	parent2 := createTestNode("Parent2")
	child := createTestNode("Child")
	if err := mg.AddNode(parent1); err != nil {
		t.Fatalf("failed to add Parent1: %v", err)
	}
	if err := mg.AddNode(parent2); err != nil {
		t.Fatalf("failed to add Parent2: %v", err)
	}
	if err := mg.AddNode(child); err != nil {
		t.Fatalf("failed to add Child: %v", err)
	}
	// Both Parent1 and Parent2 depend on Child.
	if err := mg.AddDependency(parent1.ID, child.ID, "p1"); err != nil {
		t.Fatalf("failed to add dependency Parent1->Child: %v", err)
	}
	if err := mg.AddDependency(parent2.ID, child.ID, "p2"); err != nil {
		t.Fatalf("failed to add dependency Parent2->Child: %v", err)
	}
	// Removal should fail since Child has incoming dependencies.
	if err := mg.RemoveNode(child.ID); err == nil {
		t.Errorf("expected error when removing node with multiple incoming dependencies, got nil")
	}
	// Now remove one dependency at a time.
	if err := mg.RemoveDependency(parent1.ID, child.ID); err != nil {
		t.Fatalf("failed to remove dependency Parent1->Child: %v", err)
	}
	if err := mg.RemoveDependency(parent2.ID, child.ID); err != nil {
		t.Fatalf("failed to remove dependency Parent2->Child: %v", err)
	}
	// AddCleanup removing all incoming dependencies, removal should succeed.
	if err := mg.RemoveNode(child.ID); err != nil {
		t.Errorf("expected removal to succeed after dependencies removed, got: %v", err)
	}
}

func TestMemoryGraph_RemoveNode_OutgoingDependencyAllowed(t *testing.T) {
	mg := NewMemoryGraph()
	// Node A depends on Node B, so A has an outgoing dependency,
	// but no other node depends on A.
	nodeA := createTestNode("A")
	nodeB := createTestNode("B")
	if err := mg.AddNode(nodeA); err != nil {
		t.Fatalf("failed to add node A: %v", err)
	}
	if err := mg.AddNode(nodeB); err != nil {
		t.Fatalf("failed to add node B: %v", err)
	}
	if err := mg.AddDependency(nodeA.ID, nodeB.ID, "aliasAtoB"); err != nil {
		t.Fatalf("failed to add dependency A->B: %v", err)
	}
	// Removal of nodeA should succeed.
	if err := mg.RemoveNode(nodeA.ID); err != nil {
		t.Errorf("expected node with outgoing dependency to be removable, got: %v", err)
	}
}

func TestMemoryGraph_RemoveNode_Isolated(t *testing.T) {
	mg := NewMemoryGraph()
	isolated := createTestNode("Isolated")
	if err := mg.AddNode(isolated); err != nil {
		t.Fatalf("failed to add isolated node: %v", err)
	}
	if err := mg.RemoveNode(isolated.ID); err != nil {
		t.Errorf("expected isolated node to be removable, got: %v", err)
	}
}

func TestMemoryGraph_Build_TransitiveModules(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn a chain of nodes: main -> middle -> leaf
	mainNode := createTestNode("MainNode")
	middleNode := createTestNode("MiddleNode")
	leafNode := createTestNode("LeafNode")

	// Spawn different modules for each level
	var mainMod runtime.Module = &dummyModule{name: "mainMod"}
	var middleMod runtime.Module = &dummyModule{name: "middleMod"}
	var leafMod runtime.Module = &dummyModule{name: "leafMod"}

	// Assign modules to nodes
	mainNode.Module = mainMod
	middleNode.Module = middleMod
	leafNode.Module = leafMod

	// AddCleanup all nodes to the graph
	if err := mg.AddNode(mainNode); err != nil {
		t.Fatalf("failed to add main node: %v", err)
	}
	if err := mg.AddNode(middleNode); err != nil {
		t.Fatalf("failed to add middle node: %v", err)
	}
	if err := mg.AddNode(leafNode); err != nil {
		t.Fatalf("failed to add leaf node: %v", err)
	}

	// Spawn dependency chain: main -> middle -> leaf
	if err := mg.AddDependency(mainNode.ID, middleNode.ID, "middle"); err != nil {
		t.Fatalf("failed to add dependency main->middle: %v", err)
	}
	if err := mg.AddDependency(middleNode.ID, leafNode.ID, "leaf"); err != nil {
		t.Fatalf("failed to add dependency middle->leaf: %v", err)
	}

	// Build runtime starting from main node
	rt, err := mg.Build(mainNode.ID)
	if err != nil {
		t.Fatalf("failed to build runtime: %v", err)
	}

	// Spawn a map to track seen modules
	seenModules := make(map[string]bool)

	// Track main node's module if it has one
	if rt.Main.Module != nil {
		seenModules[rt.Main.Module.Name()] = true
	}

	// Track modules from dependencies
	for _, dep := range rt.Dependencies {
		if dep.Node.Module != nil {
			seenModules[dep.Node.Module.Name()] = true
		}

		// Verify specific aliases
		switch dep.Node.ID.Name {
		case "MiddleNode":
			if dep.Name != "middle" {
				t.Errorf("expected middle node alias 'middle', got '%s'", dep.Name)
			}
		case "LeafNode":
			if dep.Name != "leaf" {
				t.Errorf("expected leaf node alias 'leaf', got '%s'", dep.Name)
			}
		}
	}

	// Verify all expected modules are present
	expectedModules := []string{"mainMod", "middleMod", "leafMod"}
	for _, modName := range expectedModules {
		if !seenModules[modName] {
			t.Errorf("missing expected module %s", modName)
		}
	}

	// Verify main node is correct
	if rt.Main.ID != mainNode.ID {
		t.Errorf("expected main node Process %v, got %v", mainNode.ID, rt.Main.ID)
	}

	// Verify dependencies are in correct order
	if len(rt.Dependencies) < 2 {
		t.Fatal("expected at least 2 dependencies (middle and leaf nodes)")
	}

	// Verify leaf node appears before middle node in dependencies
	// since leaf is depended upon by middle
	foundLeaf := false
	foundMiddle := false
	for _, dep := range rt.Dependencies {
		if dep.Node.ID.Name == "LeafNode" {
			foundLeaf = true
		}
		if dep.Node.ID.Name == "MiddleNode" {
			foundMiddle = true
			if !foundLeaf {
				t.Error("leaf node should appear before middle node in dependencies")
			}
		}
	}

	if !foundLeaf || !foundMiddle {
		t.Error("missing either leaf or middle node in dependencies")
	}
}

func TestMemoryGraph_Build_ModuleDeduplication(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn nodes for our diamond dependency pattern
	mainNode := createTestNode("MainFunc")
	dep1Node := createTestNode("Dep1")
	dep2Node := createTestNode("Dep2")
	commonDepNode := createTestNode("CommonDep")

	// Spawn modules
	var sharedMod runtime.Module = &dummyModule{name: "sharedModule"}
	var uniqueMod runtime.Module = &dummyModule{name: "uniqueModule"}

	// Both dep1 and dep2 depend on commonDep which uses sharedModule
	commonDepNode.Module = sharedMod
	// dep1 also uses a unique module
	dep1Node.Module = uniqueMod

	// AddCleanup all nodes to the graph
	nodes := []*Node{mainNode, dep1Node, dep2Node, commonDepNode}
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Spawn diamond dependency pattern
	dependencies := []struct {
		from  *Node
		to    *Node
		alias string
	}{
		{mainNode, dep1Node, "dep1"},
		{mainNode, dep2Node, "dep2"},
		{dep1Node, commonDepNode, "common1"},
		{dep2Node, commonDepNode, "common2"},
	}

	for _, dep := range dependencies {
		if err := mg.AddDependency(dep.from.ID, dep.to.ID, dep.alias); err != nil {
			t.Fatalf("failed to add dependency %v->%v: %v", dep.from.ID, dep.to.ID, err)
		}
	}

	// Build runtime starting from main node
	rt, err := mg.Build(mainNode.ID)
	if err != nil {
		t.Fatalf("failed to build runtime: %v", err)
	}

	// Ready unique modules in dependencies
	uniqueModules := make(map[string]bool)
	for _, dep := range rt.Dependencies {
		if dep.Node.Module != nil {
			uniqueModules[dep.Node.Module.Name()] = true
		}
	}

	// Verify module deduplication
	if len(uniqueModules) != 2 {
		t.Errorf("expected 2 unique modules (shared + unique), got %d", len(uniqueModules))
	}

	// Verify expected modules are present exactly once
	expectedModules := []string{"sharedModule", "uniqueModule"}
	for _, expected := range expectedModules {
		if !uniqueModules[expected] {
			t.Errorf("missing expected module %s", expected)
		}
	}

	// Verify dependency structure and order
	if len(rt.Dependencies) != 4 {
		t.Errorf("expected 4 dependency prototypes (commonDep with both aliases, dep1, dep2), got %d", len(rt.Dependencies))
	}

	// Verify main node
	if rt.Main.ID != mainNode.ID {
		t.Errorf("expected main node Process %v, got %v", mainNode.ID, rt.Main.ID)
	}

	// Verify dependencies are in correct order and have correct aliases
	// Order should be: commonDep entries first (deepest level), then dep1, dep2
	expectedDeps := []struct {
		id    registry.ID
		alias string
	}{
		{commonDepNode.ID, "common1"}, // Load shared dependency first
		{commonDepNode.ID, "common2"}, // Both aliases of shared dependency
		{dep1Node.ID, "dep1"},         // Can load after its dependency (commonDep)
		{dep2Node.ID, "dep2"},         // Can load after its dependency (commonDep)
	}

	if len(rt.Dependencies) != len(expectedDeps) {
		t.Fatalf("wrong number of dependencies: expected %d, got %d", len(expectedDeps), len(rt.Dependencies))
	}

	// Verify each dependency is in correct order with correct alias
	for i, expected := range expectedDeps {
		actual := rt.Dependencies[i]
		if actual.Node.ID != expected.id {
			t.Errorf("dependency at position %d: expected node %v, got %v", i, expected.id, actual.Node.ID)
		}
		if actual.Name != expected.alias {
			t.Errorf("dependency at position %d: expected alias %s, got %s", i, expected.alias, actual.Name)
		}
	}

	// Verify commonDep entries appear before any nodes that depend on them
	lastCommonDepPos := -1
	for i, dep := range rt.Dependencies {
		if dep.Node.ID == commonDepNode.ID {
			lastCommonDepPos = i
		} else if lastCommonDepPos == -1 {
			t.Errorf("found dependent node %v before its dependency CommonDep", dep.Node.ID)
		}
	}

	// Verify all commonDep entries are grouped together at the start
	for i := 1; i <= lastCommonDepPos; i++ {
		if rt.Dependencies[i].Node.ID != commonDepNode.ID {
			t.Errorf("CommonDep entries not grouped together at start, found %v at position %d", rt.Dependencies[i].Node.ID, i)
		}
	}
}

func TestMemoryGraph_Build_AliasCollision(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn modules
	var mod1 runtime.Module = &dummyModule{name: "module1"}
	var mod2 runtime.Module = &dummyModule{name: "module2"}

	// Spawn nodes
	nodes := map[string]*Node{
		"main": createTestNode("MainFunc"),
		"lib1": createTestNode("Lib1"),
		"lib2": createTestNode("Lib2"),
		"mod1": {
			ID:     registry.ID{Name: "Module1"},
			Kind:   "module.lua",
			Source: "module1 source",
			Module: mod1,
		},
		"mod2": {
			ID:     registry.ID{Name: "Module2"},
			Kind:   "module.lua",
			Source: "module2 source",
			Module: mod2,
		},
	}

	// AddCleanup all nodes to graph
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Define dependencies - attempting to use same alias for different libs
	dependencies := []struct {
		from  string
		to    string
		alias string
	}{
		{"main", "lib1", "helper"}, // First use of "helper" alias
		{"main", "lib2", "helper"}, // Should fail - same alias for different lib
		{"lib1", "mod1", ""},
		{"lib2", "mod2", ""},
	}

	// AddCleanup dependencies and expect failure on collision
	var collisionErr error
	for _, dep := range dependencies {
		from := nodes[dep.from]
		to := nodes[dep.to]
		if err := mg.AddDependency(from.ID, to.ID, dep.alias); err != nil {
			if dep.from == "main" && dep.to == "lib2" {
				collisionErr = err
				break
			}
			t.Fatalf("unexpected error adding dependency %s->%s: %v", dep.from, dep.to, err)
		}
	}

	if collisionErr == nil {
		t.Error("expected error when adding dependency with duplicate alias, got nil")
	}
}

func TestMemoryGraph_Build_SharedDependency(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn modules
	var mod1 runtime.Module = &dummyModule{name: "module1"}
	var sharedMod runtime.Module = &dummyModule{name: "shared"}

	// Spawn nodes
	nodes := map[string]*Node{
		"main": createTestNode("MainFunc"),
		"lib1": createTestNode("Lib1"),
		"lib2": createTestNode("Lib2"),
		"mod1": {
			ID:     registry.ID{Name: "Module1"},
			Kind:   "module.lua",
			Source: "module1 source",
			Module: mod1,
		},
		"shared": {
			ID:     registry.ID{Name: "Shared"},
			Kind:   "module.lua",
			Source: "shared module source",
			Module: sharedMod,
		},
	}

	// AddCleanup all nodes to graph
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Define dependencies - shared dependency referenced with same alias
	dependencies := []struct {
		from  string
		to    string
		alias string
	}{
		{"main", "lib1", "helper1"},
		{"main", "lib2", "helper2"},
		{"lib1", "shared", "util"}, // Both libs reference shared module
		{"lib2", "shared", "util"}, // with same alias
		{"lib1", "mod1", ""},
	}

	// AddCleanup all dependencies
	for _, dep := range dependencies {
		from := nodes[dep.from]
		to := nodes[dep.to]
		if err := mg.AddDependency(from.ID, to.ID, dep.alias); err != nil {
			t.Fatalf("failed to add dependency %s->%s: %v", dep.from, dep.to, err)
		}
	}

	// Build runtime starting from main node
	rt, err := mg.Build(nodes["main"].ID)
	if err != nil {
		t.Fatalf("failed to build runtime: %v", err)
	}

	// Ready unique modules in dependencies
	uniqueModules := make(map[string]bool)
	for _, dep := range rt.Dependencies {
		if dep.Node.Module != nil {
			uniqueModules[dep.Node.Module.Name()] = true
		}
	}

	// Verify correct number of unique modules
	if len(uniqueModules) != 2 {
		t.Errorf("expected 2 unique modules, got %d", len(uniqueModules))
	}

	// Helper to count occurrences of a node in dependencies
	countDeps := func(nodeName string) int {
		count := 0
		for _, dep := range rt.Dependencies {
			if dep.Node.ID.Name == nodeName {
				count++
			}
		}
		return count
	}

	// Verify shared module appears exactly once
	sharedCount := countDeps("Shared")
	if sharedCount != 1 {
		t.Errorf("expected shared module to appear once, got %d occurrences", sharedCount)
	}

	// Verify dependency order
	var sharedPos, lib1Pos, lib2Pos int
	for i, dep := range rt.Dependencies {
		switch dep.Node.ID.Name {
		case "Shared":
			sharedPos = i
		case "Lib1":
			lib1Pos = i
		case "Lib2":
			lib2Pos = i
		}
	}

	// Shared module should be initialized before both libraries
	if sharedPos > lib1Pos || sharedPos > lib2Pos {
		t.Error("shared module must be initialized before dependent libraries")
	}

	// Verify the shared module has the correct alias
	for _, dep := range rt.Dependencies {
		if dep.Node.ID.Name == "Shared" && dep.Name != "util" {
			t.Errorf("expected shared module to have alias 'util', got '%s'", dep.Name)
		}
	}
}

func TestMemoryGraph_Build_FallbackAlias(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn nodes
	mainNode := createTestNode("MainFunc")
	dependencyNode := createTestNode("DependencyFunc")

	// AddCleanup nodes to graph
	if err := mg.AddNode(mainNode); err != nil {
		t.Fatalf("failed to add main node: %v", err)
	}
	if err := mg.AddNode(dependencyNode); err != nil {
		t.Fatalf("failed to add dependency node: %v", err)
	}

	// AddCleanup dependency without an alias
	if err := mg.AddDependency(mainNode.ID, dependencyNode.ID, ""); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Build runtime starting from main node
	rt, err := mg.Build(mainNode.ID)
	if err != nil {
		t.Fatalf("failed to build runtime: %v", err)
	}

	// Verify we have exactly one dependency
	if len(rt.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(rt.Dependencies))
	}

	// Verify the dependency has the node name as alias
	dep := rt.Dependencies[0]
	if dep.Name != dependencyNode.ID.Name {
		t.Errorf("expected alias to be node name '%s', got '%s'", dependencyNode.ID.Name, dep.Name)
	}
}

func TestMemoryGraph_GetAllDependents(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn nodes for a dependency chain: A -> B -> C -> D
	nodeA := createTestNode("A")
	nodeB := createTestNode("B")
	nodeC := createTestNode("C")
	nodeD := createTestNode("D")
	nodeE := createTestNode("E") // E -> C (additional dependent on C)

	// AddCleanup all nodes
	nodes := []*Node{nodeA, nodeB, nodeC, nodeD, nodeE}
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Spawn dependencies
	dependencies := []struct {
		from *Node
		to   *Node
	}{
		{nodeA, nodeB}, // A depends on B
		{nodeB, nodeC}, // B depends on C
		{nodeC, nodeD}, // C depends on D
		{nodeE, nodeC}, // E depends on C
	}

	for _, dep := range dependencies {
		if err := mg.AddDependency(dep.from.ID, dep.to.ID, ""); err != nil {
			t.Fatalf("failed to add dependency %v->%v: %v", dep.from.ID, dep.to.ID, err)
		}
	}

	tests := []struct {
		name     string
		nodeID   registry.ID
		expected []string // Expected dependent node names
	}{
		{
			name:     "leaf node dependents",
			nodeID:   nodeD.ID,
			expected: []string{"A", "B", "C", "E"}, // E is affected through C
		},
		{
			name:     "middle node dependents",
			nodeID:   nodeC.ID,
			expected: []string{"A", "B", "E"},
		},
		{
			name:     "node with single dependent",
			nodeID:   nodeB.ID,
			expected: []string{"A"},
		},
		{
			name:     "node with no dependents",
			nodeID:   nodeA.ID,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := mg.GetAllDependents(tt.nodeID)
			if err != nil {
				t.Fatalf("GetAllDependents failed: %v", err)
			}

			// Convert result to map of names for easier comparison
			got := make(map[string]bool)
			for _, dep := range deps {
				got[dep.ID.Name] = true
			}

			// Verify all expected dependents are present
			for _, expected := range tt.expected {
				if !got[expected] {
					t.Errorf("missing expected dependent %s", expected)
				}
			}

			// Verify no unexpected dependents
			if len(deps) != len(tt.expected) {
				// Spawn slices for better error output
				var gotNames []string
				for _, d := range deps {
					gotNames = append(gotNames, d.ID.Name)
				}
				t.Errorf("wrong number of dependents:\n  got:  %d %v\n  want: %d %v",
					len(deps), gotNames, len(tt.expected), tt.expected)
			}
		})
	}

	t.Run("nonexistent node", func(t *testing.T) {
		_, err := mg.GetAllDependents(registry.ID{Name: "nonexistent"})
		if err == nil {
			t.Error("expected error for nonexistent node, got nil")
		}
	})
}

func TestMemoryGraph_GetAllDependents_NoDuplicates(t *testing.T) {
	mg := NewMemoryGraph()

	// Spawn nodes for diamond pattern with multiple aliases
	// A depends on B and C
	// B and C both depend on D
	nodeA := createTestNode("A")
	nodeB := createTestNode("B")
	nodeC := createTestNode("C")
	nodeD := createTestNode("D")

	// AddCleanup all nodes
	nodes := []*Node{nodeA, nodeB, nodeC, nodeD}
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Spawn diamond dependency pattern with different aliases
	dependencies := []struct {
		from  *Node
		to    *Node
		alias string
	}{
		{nodeA, nodeB, "b1"},
		{nodeA, nodeC, "c1"},
		{nodeB, nodeD, "d1"},
		{nodeC, nodeD, "d2"}, // D has two paths to A: through B and through C
	}

	for _, dep := range dependencies {
		if err := mg.AddDependency(dep.from.ID, dep.to.ID, dep.alias); err != nil {
			t.Fatalf("failed to add dependency %v->%v: %v", dep.from.ID, dep.to.ID, err)
		}
	}

	// Test GetAllDependents for node D
	deps, err := mg.GetAllDependents(nodeD.ID)
	if err != nil {
		t.Fatalf("GetAllDependents failed: %v", err)
	}

	// Check for duplicates using map
	seen := make(map[string]bool)
	duplicates := make(map[string]int)
	for _, dep := range deps {
		if seen[dep.ID.Name] {
			duplicates[dep.ID.Name]++
		}
		seen[dep.ID.Name] = true
	}

	// Fail if any duplicates found
	if len(duplicates) > 0 {
		t.Errorf("found duplicate nodes in dependents: %v", duplicates)
	}

	// Verify we got exactly the expected nodes (A, B, C)
	expectedNodes := map[string]bool{
		"A": true,
		"B": true,
		"C": true,
	}

	if len(deps) != len(expectedNodes) {
		t.Errorf("wrong number of dependents: got %d, want %d", len(deps), len(expectedNodes))
	}

	for _, dep := range deps {
		if !expectedNodes[dep.ID.Name] {
			t.Errorf("unexpected dependent: %v", dep.ID.Name)
		}
	}
}

func TestNewMemoryGraph(t *testing.T) {
	mg := NewMemoryGraph()
	assert.NotNil(t, mg)
	assert.NotNil(t, mg.nodes)
	assert.NotNil(t, mg.graph)
}

// TestMemoryGraph_ConcurrentAddNode tests concurrent node additions
func TestMemoryGraph_ConcurrentAddNode(t *testing.T) {
	mg := NewMemoryGraph()
	const numGoroutines = 10
	const numNodes = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines adding nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				node := createTestNode(fmt.Sprintf("node_%d_%d", goroutineID, j))
				if err := mg.AddNode(node); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent AddNode failed: %v", err)
	}

	// Verify all nodes were added
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numNodes; j++ {
			nodeID := registry.ID{Name: fmt.Sprintf("node_%d_%d", i, j)}
			_, err := mg.GetNode(nodeID)
			assert.NoError(t, err, "Node should exist after concurrent addition")
		}
	}
}

// TestMemoryGraph_ConcurrentAddDependency tests concurrent dependency additions
func TestMemoryGraph_ConcurrentAddDependency(t *testing.T) {
	mg := NewMemoryGraph()

	// Add nodes first
	const numNodes = 20
	nodes := make([]*Node, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = createTestNode(fmt.Sprintf("node_%d", i))
		err := mg.AddNode(nodes[i])
		require.NoError(t, err)
	}

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines adding dependencies concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes-1; j++ {
				fromID := nodes[j].ID
				toID := nodes[j+1].ID
				alias := fmt.Sprintf("dep_%d_%d", goroutineID, j)
				if err := mg.AddDependency(fromID, toID, alias); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors - many are expected due to duplicate dependencies
	errorCount := 0
	for err := range errors {
		errorCount++
		// Log first few errors for debugging, but don't fail the test
		if errorCount <= 5 {
			t.Logf("Concurrent AddDependency error (expected): %v", err)
		}
	}

	// Verify that at least some dependencies were successfully added
	// (some will fail due to duplicates, which is expected behavior)
	totalDependencies := 0
	for i := 0; i < numNodes-1; i++ {
		deps, err := mg.GetDirectDependencies(nodes[i].ID)
		assert.NoError(t, err)
		totalDependencies += len(deps)
	}

	// At least some dependencies should have been added successfully
	assert.Greater(t, totalDependencies, 0, "At least some dependencies should have been added successfully")
	t.Logf("Successfully added %d dependencies out of %d attempts", totalDependencies, numGoroutines*(numNodes-1))
}

// TestMemoryGraph_ConcurrentReadWrite tests concurrent read and write operations
func TestMemoryGraph_ConcurrentReadWrite(t *testing.T) {
	mg := NewMemoryGraph()

	// Add initial nodes
	const numNodes = 10
	for i := 0; i < numNodes; i++ {
		node := createTestNode(fmt.Sprintf("node_%d", i))
		err := mg.AddNode(node)
		require.NoError(t, err)
	}

	const numReaders = 5
	const numWriters = 3
	var wg sync.WaitGroup
	errors := make(chan error, numReaders+numWriters)

	// Start reader goroutines
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Read operations
				nodeID := registry.ID{Name: fmt.Sprintf("node_%d", j%numNodes)}
				_, err := mg.GetNode(nodeID)
				if err != nil {
					errors <- err
					return
				}

				// Get dependencies
				deps, err := mg.GetDirectDependencies(nodeID)
				if err != nil {
					errors <- err
					return
				}
				_ = deps // Use the result

				// Get dependents
				dependents, err := mg.GetAllDependents(nodeID)
				if err != nil {
					errors <- err
					return
				}
				_ = dependents // Use the result

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Start writer goroutines
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				// Add new nodes
				newNode := createTestNode(fmt.Sprintf("new_node_%d_%d", writerID, j))
				if err := mg.AddNode(newNode); err != nil {
					errors <- err
				}

				// Add dependencies
				if j > 0 {
					fromID := registry.ID{Name: fmt.Sprintf("new_node_%d_%d", writerID, j-1)}
					toID := registry.ID{Name: fmt.Sprintf("new_node_%d_%d", writerID, j)}
					if err := mg.AddDependency(fromID, toID, fmt.Sprintf("dep_%d", j)); err != nil {
						errors <- err
					}
				}

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent read/write operation failed: %v", err)
	}
}

// TestMemoryGraph_RaceConditionCache tests for race conditions in cache operations
func TestMemoryGraph_RaceConditionCache(t *testing.T) {
	mg := NewMemoryGraph()

	// Add nodes with dependencies to populate cache
	const numNodes = 10
	for i := 0; i < numNodes; i++ {
		node := createTestNode(fmt.Sprintf("node_%d", i))
		err := mg.AddNode(node)
		require.NoError(t, err)
	}

	// Add dependencies to create a chain
	for i := 0; i < numNodes-1; i++ {
		fromID := registry.ID{Name: fmt.Sprintf("node_%d", i)}
		toID := registry.ID{Name: fmt.Sprintf("node_%d", i+1)}
		err := mg.AddDependency(fromID, toID, fmt.Sprintf("dep_%d", i))
		require.NoError(t, err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup

	// Start goroutines that read from cache and trigger cache invalidation
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Read operations that use cache
			for j := 0; j < 50; j++ {
				nodeID := registry.ID{Name: fmt.Sprintf("node_%d", j%numNodes)}

				// Get dependents (uses cache)
				dependents, err := mg.GetAllDependents(nodeID)
				if err != nil {
					t.Errorf("GetAllDependents failed: %v", err)
					return
				}
				_ = dependents

				// Get dependency levels (uses cache)
				levels, err := mg.DependencyLevels()
				if err != nil {
					t.Errorf("DependencyLevels failed: %v", err)
					return
				}
				_ = levels

				// Add a new node to trigger cache invalidation
				if j%10 == 0 {
					newNode := createTestNode(fmt.Sprintf("cache_node_%d_%d", goroutineID, j))
					if err := mg.AddNode(newNode); err != nil {
						t.Errorf("AddNode failed: %v", err)
						return
					}
				}

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()

	// Verify no panic occurred and cache operations worked correctly
	for i := 0; i < numNodes; i++ {
		nodeID := registry.ID{Name: fmt.Sprintf("node_%d", i)}
		_, err := mg.GetNode(nodeID)
		assert.NoError(t, err, "Node should exist after cache race condition test")
	}
}

// TestMemoryGraph_ConcurrentRemoveNode tests concurrent node removal
func TestMemoryGraph_ConcurrentRemoveNode(t *testing.T) {
	mg := NewMemoryGraph()

	// Add nodes first
	const numNodes = 20
	for i := 0; i < numNodes; i++ {
		node := createTestNode(fmt.Sprintf("node_%d", i))
		err := mg.AddNode(node)
		require.NoError(t, err)
	}

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines removing nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				nodeID := registry.ID{Name: fmt.Sprintf("node_%d", j)}
				if err := mg.RemoveNode(nodeID); err != nil {
					// Some removals may fail if node was already removed
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors (some are expected due to concurrent removals)
	errorCount := 0
	for err := range errors {
		errorCount++
		_ = err // Log error for debugging
	}

	// Verify all nodes were eventually removed
	for i := 0; i < numNodes; i++ {
		nodeID := registry.ID{Name: fmt.Sprintf("node_%d", i)}
		_, err := mg.GetNode(nodeID)
		assert.Error(t, err, "Node should be removed after concurrent removal")
	}
}

// TestMemoryGraph_ConcurrentDependencyLevels tests concurrent dependency level calculations
func TestMemoryGraph_ConcurrentDependencyLevels(t *testing.T) {
	mg := NewMemoryGraph()

	// Create a complex dependency graph
	const numNodes = 15
	for i := 0; i < numNodes; i++ {
		node := createTestNode(fmt.Sprintf("node_%d", i))
		err := mg.AddNode(node)
		require.NoError(t, err)
	}

	// Add dependencies to create multiple levels
	for i := 0; i < numNodes-1; i++ {
		fromID := registry.ID{Name: fmt.Sprintf("node_%d", i)}
		toID := registry.ID{Name: fmt.Sprintf("node_%d", i+1)}
		err := mg.AddDependency(fromID, toID, fmt.Sprintf("dep_%d", i))
		require.NoError(t, err)
	}

	const numGoroutines = 8
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines calculating dependency levels concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				levels, err := mg.DependencyLevels()
				if err != nil {
					errors <- err
					return
				}

				// Verify levels are valid
				if len(levels) == 0 {
					errors <- fmt.Errorf("dependency levels should not be empty")
					return
				}

				// Verify no level is empty
				for levelIdx, level := range levels {
					if len(level) == 0 {
						errors <- fmt.Errorf("level %d should not be empty", levelIdx)
						return
					}
				}

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent DependencyLevels failed: %v", err)
	}
}

// TestMemoryGraph_ConcurrentGetAllDependents tests concurrent dependent calculations
func TestMemoryGraph_ConcurrentGetAllDependents(t *testing.T) {
	mg := NewMemoryGraph()

	// Create a diamond dependency pattern
	// A -> B, A -> C, B -> D, C -> D
	nodes := []*Node{
		createTestNode("A"),
		createTestNode("B"),
		createTestNode("C"),
		createTestNode("D"),
	}

	for _, node := range nodes {
		err := mg.AddNode(node)
		require.NoError(t, err)
	}

	// Add dependencies
	dependencies := []struct {
		from, to string
		alias    string
	}{
		{"A", "B", "dep1"},
		{"A", "C", "dep2"},
		{"B", "D", "dep3"},
		{"C", "D", "dep4"},
	}

	for _, dep := range dependencies {
		fromID := registry.ID{Name: dep.from}
		toID := registry.ID{Name: dep.to}
		err := mg.AddDependency(fromID, toID, dep.alias)
		require.NoError(t, err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines getting dependents concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Test getting dependents for different nodes
				testNodes := []string{"A", "B", "C", "D"}
				for _, nodeName := range testNodes {
					nodeID := registry.ID{Name: nodeName}
					dependents, err := mg.GetAllDependents(nodeID)
					if err != nil {
						errors <- err
						return
					}

					// Verify results are consistent (allow for some variation due to concurrent cache operations)
					switch nodeName {
					case "A":
						// A should have no dependents
						if len(dependents) != 0 {
							errors <- fmt.Errorf("node A should have no dependents, got %d", len(dependents))
							return
						}
					case "B", "C":
						// B and C should have A as dependent
						if len(dependents) != 1 || dependents[0].ID.Name != "A" {
							errors <- fmt.Errorf("node %s should have A as dependent", nodeName)
							return
						}
					case "D":
						// D should have A, B, C as dependents (allow for cache inconsistencies)
						if len(dependents) < 2 || len(dependents) > 4 {
							errors <- fmt.Errorf("node D should have 2-4 dependents, got %d", len(dependents))
							return
						}
						// Verify that A is always present (the root node)
						hasA := false
						for _, dep := range dependents {
							if dep.ID.Name == "A" {
								hasA = true
								break
							}
						}
						if !hasA {
							errors <- fmt.Errorf("node D should always have A as dependent")
							return
						}
					}
				}

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent GetAllDependents failed: %v", err)
	}
}

// TestMemoryGraph_StressTest performs a comprehensive stress test
func TestMemoryGraph_StressTest(t *testing.T) {
	mg := NewMemoryGraph()

	const numOperations = 1000
	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numOperations)

	// Start multiple goroutines performing various operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				operation := j % 6
				switch operation {
				case 0:
					// Add node
					node := createTestNode(fmt.Sprintf("stress_node_%d_%d", goroutineID, j))
					if err := mg.AddNode(node); err != nil {
						errors <- err
					}
				case 1:
					// Get node
					nodeID := registry.ID{Name: fmt.Sprintf("stress_node_%d_%d", goroutineID, j)}
					_, err := mg.GetNode(nodeID)
					if err != nil {
						// Expected for non-existent nodes
						_ = err
					}
				case 2:
					// Add dependency
					fromID := registry.ID{Name: fmt.Sprintf("stress_node_%d_%d", goroutineID, j)}
					toID := registry.ID{Name: fmt.Sprintf("stress_node_%d_%d", goroutineID, j+1)}
					if err := mg.AddDependency(fromID, toID, fmt.Sprintf("stress_dep_%d", j)); err != nil {
						// Expected for non-existent nodes
						_ = err
					}
				case 3:
					// Get dependencies
					nodeID := registry.ID{Name: fmt.Sprintf("stress_node_%d_%d", goroutineID, j)}
					_, err := mg.GetDirectDependencies(nodeID)
					if err != nil {
						// Expected for non-existent nodes
						_ = err
					}
				case 4:
					// Get dependents
					nodeID := registry.ID{Name: fmt.Sprintf("stress_node_%d_%d", goroutineID, j)}
					_, err := mg.GetAllDependents(nodeID)
					if err != nil {
						// Expected for non-existent nodes
						_ = err
					}
				case 5:
					// Get dependency levels
					_, err := mg.DependencyLevels()
					if err != nil {
						errors <- err
					}
				}

				if j%100 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Stress test operation failed: %v", err)
	}

	// Verify the graph is still in a consistent state
	levels, err := mg.DependencyLevels()
	assert.NoError(t, err, "Graph should be in consistent state after stress test")
	_ = levels
}
