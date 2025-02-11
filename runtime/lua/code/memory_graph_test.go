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

// TestMemoryGraph_BuildRuntime tests construction of a Main configuration.
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
		rt, err := mg.Build(nodeA.ID)
		if err != nil {
			t.Fatalf("failed to build runtime: %v", err)
		}
		// Verify main node.
		if rt.Main.ID != nodeA.ID {
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
	// After removing all incoming dependencies, removal should succeed.
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

	// Create a chain of nodes: main -> middle -> leaf
	mainNode := createTestNode("MainNode")
	middleNode := createTestNode("MiddleNode")
	leafNode := createTestNode("LeafNode")

	// Create different modules for each level
	var mainMod runtime.Module = &dummyModule{name: "mainMod"}
	var middleMod runtime.Module = &dummyModule{name: "middleMod"}
	var leafMod runtime.Module = &dummyModule{name: "leafMod"}

	// Assign modules to nodes
	mainNode.Module = &mainMod
	middleNode.Module = &middleMod
	leafNode.Module = &leafMod

	// Add all nodes to the graph
	if err := mg.AddNode(mainNode); err != nil {
		t.Fatalf("failed to add main node: %v", err)
	}
	if err := mg.AddNode(middleNode); err != nil {
		t.Fatalf("failed to add middle node: %v", err)
	}
	if err := mg.AddNode(leafNode); err != nil {
		t.Fatalf("failed to add leaf node: %v", err)
	}

	// Create dependency chain: main -> middle -> leaf
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

	// Verify that all modules are present
	if len(rt.Modules) != 3 {
		t.Errorf("expected 3 modules, got %d", len(rt.Modules))
	}

	// Create a map of module names for easy checking
	moduleNames := make(map[string]bool)
	for _, mod := range rt.Modules {
		moduleNames[(*mod).Name()] = true
	}

	// Verify each expected module is present
	expectedModules := []string{"mainMod", "middleMod", "leafMod"}
	for _, expected := range expectedModules {
		if !moduleNames[expected] {
			t.Errorf("missing expected module %s", expected)
		}
	}

	// Verify dependency chain is preserved
	if len(rt.DepProtos) != 2 {
		t.Errorf("expected 2 dependency prototypes, got %d", len(rt.DepProtos))
	}

	// Verify main node is correct
	if rt.Main.ID != mainNode.ID {
		t.Errorf("expected main node ID %v, got %v", mainNode.ID, rt.Main.ID)
	}

	// Verify aliases are preserved through the chain
	middleFound := false
	leafFound := false
	for _, dep := range rt.DepProtos {
		switch dep.Node.ID {
		case middleNode.ID:
			if dep.Alias != "middle" {
				t.Errorf("expected middle node alias 'middle', got '%s'", dep.Alias)
			}
			middleFound = true
		case leafNode.ID:
			if dep.Alias != "leaf" {
				t.Errorf("expected leaf node alias 'leaf', got '%s'", dep.Alias)
			}
			leafFound = true
		}
	}

	if !middleFound {
		t.Error("middle node not found in dependencies")
	}
	if !leafFound {
		t.Error("leaf node not found in dependencies")
	}
}

func TestMemoryGraph_Build_ModuleDeduplication(t *testing.T) {
	mg := NewMemoryGraph()

	// Create nodes for our diamond dependency pattern:
	// main -> dep1 -> commonDep
	//     \-> dep2 -> commonDep
	mainNode := createTestNode("MainFunc")
	dep1Node := createTestNode("Dep1")
	dep2Node := createTestNode("Dep2")
	commonDepNode := createTestNode("CommonDep")

	// Create modules
	var sharedMod runtime.Module = &dummyModule{name: "sharedModule"}
	var uniqueMod runtime.Module = &dummyModule{name: "uniqueModule"}

	// Both dep1 and dep2 depend on commonDep which uses sharedModule
	commonDepNode.Module = &sharedMod
	// dep1 also uses a unique module
	dep1Node.Module = &uniqueMod

	// Add all nodes to the graph
	nodes := []*Node{mainNode, dep1Node, dep2Node, commonDepNode}
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Create diamond dependency pattern
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

	// Verify module deduplication
	if len(rt.Modules) != 2 {
		t.Errorf("expected 2 unique modules (shared + unique), got %d", len(rt.Modules))
	}

	// Create a map of module names for verification
	moduleNames := make(map[string]bool)
	for _, mod := range rt.Modules {
		moduleNames[(*mod).Name()] = true
	}

	// Verify expected modules are present exactly once
	expectedModules := []string{"sharedModule", "uniqueModule"}
	for _, expected := range expectedModules {
		if !moduleNames[expected] {
			t.Errorf("missing expected module %s", expected)
		}
	}

	// Verify dependency structure and order
	if len(rt.DepProtos) != 4 {
		t.Errorf("expected 4 dependency prototypes (commonDep with both aliases, dep1, dep2), got %d", len(rt.DepProtos))
	}

	// Verify main node
	if rt.Main.ID != mainNode.ID {
		t.Errorf("expected main node ID %v, got %v", mainNode.ID, rt.Main.ID)
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

	if len(rt.DepProtos) != len(expectedDeps) {
		t.Fatalf("wrong number of dependencies: expected %d, got %d", len(expectedDeps), len(rt.DepProtos))
	}

	// Verify each dependency is in correct order with correct alias
	for i, expected := range expectedDeps {
		actual := rt.DepProtos[i]
		if actual.Node.ID != expected.id {
			t.Errorf("dependency at position %d: expected node %v, got %v", i, expected.id, actual.Node.ID)
		}
		if actual.Alias != expected.alias {
			t.Errorf("dependency at position %d: expected alias %s, got %s", i, expected.alias, actual.Alias)
		}
	}

	// Verify commonDep entries appear before any nodes that depend on them
	lastCommonDepPos := -1
	for i, dep := range rt.DepProtos {
		if dep.Node.ID == commonDepNode.ID {
			lastCommonDepPos = i
		} else if lastCommonDepPos == -1 {
			t.Errorf("found dependent node %v before its dependency CommonDep", dep.Node.ID)
		}
	}

	// Verify all commonDep entries are grouped together at the start
	for i := 1; i <= lastCommonDepPos; i++ {
		if rt.DepProtos[i].Node.ID != commonDepNode.ID {
			t.Errorf("CommonDep entries not grouped together at start, found %v at position %d", rt.DepProtos[i].Node.ID, i)
		}
	}
}

func TestMemoryGraph_Build_AliasCollision(t *testing.T) {
	mg := NewMemoryGraph()

	// Create modules
	var mod1 runtime.Module = &dummyModule{name: "module1"}
	var mod2 runtime.Module = &dummyModule{name: "module2"}

	// Create nodes
	nodes := map[string]*Node{
		"main": createTestNode("MainFunc"),
		"lib1": createTestNode("Lib1"),
		"lib2": createTestNode("Lib2"),
		"mod1": &Node{
			ID:     registry.ID{Name: "Module1"},
			Kind:   "module.lua",
			Source: "module1 source",
			Module: &mod1,
		},
		"mod2": &Node{
			ID:     registry.ID{Name: "Module2"},
			Kind:   "module.lua",
			Source: "module2 source",
			Module: &mod2,
		},
	}

	// Add all nodes to graph
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

	// Add dependencies and expect failure on collision
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

	// Create modules
	var mod1 runtime.Module = &dummyModule{name: "module1"}
	var sharedMod runtime.Module = &dummyModule{name: "shared"}

	// Create nodes
	nodes := map[string]*Node{
		"main": createTestNode("MainFunc"),
		"lib1": createTestNode("Lib1"),
		"lib2": createTestNode("Lib2"),
		"mod1": &Node{
			ID:     registry.ID{Name: "Module1"},
			Kind:   "module.lua",
			Source: "module1 source",
			Module: &mod1,
		},
		"shared": &Node{
			ID:     registry.ID{Name: "Shared"},
			Kind:   "module.lua",
			Source: "shared module source",
			Module: &sharedMod,
		},
	}

	// Add all nodes to graph
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

	// Add all dependencies
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

	// Verify correct number of modules
	if len(rt.Modules) != 2 {
		t.Errorf("expected 2 unique modules, got %d", len(rt.Modules))
	}

	// Helper to count occurrences of a node in dependencies
	countDeps := func(nodeName string) int {
		count := 0
		for _, dep := range rt.DepProtos {
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
	for i, dep := range rt.DepProtos {
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
	for _, dep := range rt.DepProtos {
		if dep.Node.ID.Name == "Shared" && dep.Alias != "util" {
			t.Errorf("expected shared module to have alias 'util', got '%s'", dep.Alias)
		}
	}
}
