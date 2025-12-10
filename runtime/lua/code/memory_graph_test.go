package code

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
	runtime "github.com/wippyai/runtime/api/runtime/lua"
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

// Info returns the dummy module's metadata.
func (d *dummyModule) Info() runtime.ModuleInfo {
	return runtime.ModuleInfo{
		Name:        d.name,
		Description: "test module",
		Class:       []string{runtime.ClassDeterministic},
	}
}

// Register returns nil for test purposes.
func (d *dummyModule) Register(_ *lua.LState) *runtime.Registration {
	return nil
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
		err := mg.RemoveNode(registry.NewID("", "non-existent"))
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
		if len(deps) == 0 || deps[0].ID != nodeB.ID {
			t.Errorf("expected dependency to nodeB, got: %+v", deps)
		}
	})

	t.Run("MissingFromNode", func(t *testing.T) {
		err := mg.AddDependency(registry.NewID("", "NonExistent"), nodeB.ID, "alias")
		if err == nil {
			t.Errorf("expected error when 'from' node is missing, got nil")
		}
	})

	t.Run("MissingToNode", func(t *testing.T) {
		err := mg.AddDependency(nodeA.ID, registry.NewID("", "NonExistent"), "alias")
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
		if len(deps) == 0 || deps[0].ID != nodeB.ID {
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
		_, err := mg.GetDirectDependencies(registry.NewID("", "NonExistent"))
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
		_, err := mg.GetDirectDependents(registry.NewID("", "NonExistent"))
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
		if rt.Main == nil || rt.Main.Source == "" {
			t.Errorf("unexpected main node or source, got: %+v", rt.Main)
		}
		// Check dependency prototypes (should include nodeB and nodeC).
		if len(rt.Dependencies) != 2 {
			t.Errorf("expected 2 dependency prototypes, got %d", len(rt.Dependencies))
		}
		// Verify alias propagation for nodeB and module for nodeC.
		for _, dep := range rt.Dependencies {
			if dep.Name == "depB" && dep.Node.ID.Name != "DepNodeB" {
				t.Errorf("expected alias 'depB' for nodeB, got '%s'", dep.Name)
			}
			// Verify module is included as a dependency
			if dep.Node.ID.Name == "DepNodeC" {
				if dep.Node.Module == nil || dep.Node.Module.Info().Name != "dummyMod" {
					t.Errorf("expected module 'dummyMod' in nodeC dependency")
				}
			}
		}
	})

	t.Run("InvalidEntrypoint", func(t *testing.T) {
		_, err := mg.Build(registry.NewID("", "NonExistent"))
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
		seenModules[rt.Main.Module.Info().Name] = true
	}

	// Track modules from dependencies
	for _, dep := range rt.Dependencies {
		if dep.Node.Module != nil {
			seenModules[dep.Node.Module.Info().Name] = true
		}

		// Verify specific aliases - direct dependencies use their edge alias,
		// transitive dependencies use their parent's edge alias
		switch dep.Node.ID.Name {
		case "MiddleNode":
			if dep.Name != "middle" {
				t.Errorf("expected middle node alias 'middle', got '%s'", dep.Name)
			}
		case "LeafNode":
			// LeafNode is a transitive dep (middle->leaf), so it uses the alias from that edge
			if dep.Name != "leaf" {
				t.Errorf("expected leaf node alias 'leaf' (from parent's edge), got '%s'", dep.Name)
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
			uniqueModules[dep.Node.Module.Info().Name] = true
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
	// commonDep appears TWICE - once for each alias (common1 and common2)
	// because both dep1 and dep2 import it with different names
	if len(rt.Dependencies) != 4 {
		t.Errorf("expected 4 dependency entries (commonDep x2 for aliases, dep1, dep2), got %d", len(rt.Dependencies))
	}

	// Verify main node
	if rt.Main.ID != mainNode.ID {
		t.Errorf("expected main node Process %v, got %v", mainNode.ID, rt.Main.ID)
	}

	// Verify commonDep has both aliases
	commonDepAliases := make(map[string]bool)
	for _, dep := range rt.Dependencies {
		if dep.Node.ID.Name == "CommonDep" {
			commonDepAliases[dep.Name] = true
		}
	}
	if !commonDepAliases["common1"] {
		t.Errorf("missing alias 'common1' for commonDep")
	}
	if !commonDepAliases["common2"] {
		t.Errorf("missing alias 'common2' for commonDep")
	}

	// Verify dep1 and dep2 have their direct aliases
	directAliases := make(map[string]string)
	for _, dep := range rt.Dependencies {
		if dep.Node.ID.Name == "Dep1" || dep.Node.ID.Name == "Dep2" {
			directAliases[dep.Node.ID.Name] = dep.Name
		}
	}
	if directAliases["Dep1"] != "dep1" {
		t.Errorf("expected Dep1 alias 'dep1', got '%s'", directAliases["Dep1"])
	}
	if directAliases["Dep2"] != "dep2" {
		t.Errorf("expected Dep2 alias 'dep2', got '%s'", directAliases["Dep2"])
	}

	// Verify commonDep appears before nodes that depend on it
	commonDepPos := -1
	dep1Pos := -1
	dep2Pos := -1
	for i, dep := range rt.Dependencies {
		if dep.Node.ID.Name == "CommonDep" && commonDepPos == -1 {
			commonDepPos = i
		}
		if dep.Node.ID.Name == "Dep1" {
			dep1Pos = i
		}
		if dep.Node.ID.Name == "Dep2" {
			dep2Pos = i
		}
	}
	if commonDepPos > dep1Pos || commonDepPos > dep2Pos {
		t.Errorf("commonDep should appear before dep1 and dep2 in dependencies")
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
			ID:     registry.NewID("", "mod1"),
			Kind:   "module.lua",
			Source: "module1 source",
			Module: mod1,
		},
		"mod2": {
			ID:     registry.NewID("", "mod2"),
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
			ID:     registry.NewID("", "mod1"),
			Kind:   "module.lua",
			Source: "module1 source",
			Module: mod1,
		},
		"shared": {
			ID:     registry.NewID("", "shared"),
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
			uniqueModules[dep.Node.Module.Info().Name] = true
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
	sharedCount := countDeps("shared")
	if sharedCount != 1 {
		t.Errorf("expected shared module to appear once, got %d occurrences", sharedCount)
	}

	// Verify dependency order
	var sharedPos, lib1Pos, lib2Pos int
	for i, dep := range rt.Dependencies {
		switch dep.Node.ID.Name {
		case "shared":
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
		_, err := mg.GetAllDependents(registry.NewID("", "nonexistent"))
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

func TestMemoryGraph_Build_TransitiveAliasCollision(t *testing.T) {
	mg := NewMemoryGraph()

	// Create modules
	var modConstX runtime.Module = &dummyModule{name: "const-x"}
	var modConstY runtime.Module = &dummyModule{name: "const-y"}
	var modLibA runtime.Module = &dummyModule{name: "library-a"}

	// Create nodes
	nodes := map[string]*Node{
		"main": createTestNode("MainFunc"),
		"const-x": {
			ID:     registry.NewID("lib", "const-x"),
			Kind:   "module.lua",
			Source: "const-x source",
			Module: modConstX,
		},
		"const-y": {
			ID:     registry.NewID("lib", "const-y"),
			Kind:   "module.lua",
			Source: "const-y source",
			Module: modConstY,
		},
		"library-a": {
			ID:     registry.NewID("lib", "library-a"),
			Kind:   "module.lua",
			Source: "library-a source",
			Module: modLibA,
		},
	}

	// Add all nodes
	for _, node := range nodes {
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node %v: %v", node.ID, err)
		}
	}

	// Create dependency tree:
	// main imports const-x as "const"
	// main imports library-a as "libA"
	// library-a imports const-y as "const" <- This IS a collision because all deps are global
	dependencies := []struct {
		from  string
		to    string
		alias string
	}{
		{"main", "const-x", "const"},      // User imports const-x as "const"
		{"main", "library-a", "libA"},     // User imports library-a
		{"library-a", "const-y", "const"}, // Library-a imports const-y also as "const" - COLLISION
	}

	// Add all dependencies
	for _, dep := range dependencies {
		from := nodes[dep.from]
		to := nodes[dep.to]
		if err := mg.AddDependency(from.ID, to.ID, dep.alias); err != nil {
			t.Fatalf("failed to add dependency %s->%s: %v", dep.from, dep.to, err)
		}
	}

	// Build should FAIL with alias collision - two different nodes use same alias "const"
	_, err := mg.Build(nodes["main"].ID)
	if err == nil {
		t.Fatalf("expected collision error, but build succeeded")
	}

	// Verify it's an alias collision error
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("expected collision error, got: %v", err)
	}
}

func TestNewMemoryGraph(t *testing.T) {
	mg := NewMemoryGraph()
	assert.NotNil(t, mg)
	assert.NotNil(t, mg.nodes)
	assert.NotNil(t, mg.graph)
}
