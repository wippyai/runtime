package engine

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// ProcessFactory Tests
//
// These tests verify ProcessFactory functionality:
// 1. Basic factory creation and process spawning
// 2. Module filtering (exclude, forbid, allow)
// 3. Class-based filtering
// 4. Transition patterns from old managers
// 5. Consolidation patterns for source/bytecode

// Test module definitions

var factoryTestModuleA = &luaapi.ModuleDef{
	Name:        "test_a",
	Description: "Test module A",
	Class:       []string{luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("a"))
		return tbl, nil
	},
}

var factoryTestModuleB = &luaapi.ModuleDef{
	Name:        "test_b",
	Description: "Test module B",
	Class:       []string{luaapi.ClassIO},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("b"))
		return tbl, nil
	},
}

var factoryTestModuleNetwork = &luaapi.ModuleDef{
	Name:        "test_network",
	Description: "Test network module",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("network"))
		return tbl, nil
	},
}

var factoryMockProcessModule = &luaapi.ModuleDef{
	Name:        "process",
	Description: "Mock process module",
	Class:       []string{luaapi.ClassProcess},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 2)
		tbl.RawSetString("spawn", lua.LGoFunc(func(_ *lua.LState) int { return 0 }))
		tbl.RawSetString("send", lua.LGoFunc(func(_ *lua.LState) int { return 0 }))
		return tbl, nil
	},
}

var factoryDefaultModules = []*luaapi.ModuleDef{
	factoryTestModuleA,
	factoryTestModuleB,
}

var factoryDefaultModulesWithProcess = []*luaapi.ModuleDef{
	factoryTestModuleA,
	factoryTestModuleB,
	factoryMockProcessModule,
}

var factoryWorkflowAllowedIDs = []registry.ID{
	{Name: "json"},
	{Name: "base64"},
	{Name: "payload"},
	{Name: "workflow"},
	{Name: "channel"},
}

// Test helpers

func setupFactoryCodeManager(t *testing.T) *code.Manager {
	t.Helper()
	log := zap.NewNop()
	cm, err := code.NewCodeManager(log, nil, code.Config{
		Modules:        nil,
		ProtoCacheSize: 100,
		MainCacheSize:  100,
	})
	if err != nil {
		t.Fatalf("failed to create code manager: %v", err)
	}
	return cm
}

func addFactoryTestFunction(t *testing.T, cm *code.Manager, id registry.ID, source string) {
	t.Helper()
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: source,
		Method: "main",
	}
	if err := cm.AddNode(context.Background(), node, nil); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}
}

// Basic Factory Tests

func TestFactory_New(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	factory := NewProcessFactory(cm)
	if factory == nil {
		t.Fatal("expected non-nil factory")
	}
	if factory.code != cm {
		t.Error("code manager not set")
	}
}

func TestFactory_CreateSimple(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "simple")
	addFactoryTestFunction(t, cm, id, `return function() return test_a.value end`)

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_ReturnsReusable(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "factory")
	addFactoryTestFunction(t, cm, id, `return function() return 42 end`)

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc1, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	proc2, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() second call failed: %v", err)
	}

	if proc1 == proc2 {
		t.Error("factory should create distinct processes")
	}
}

func TestFactory_EmptyModulesList(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "empty_modules")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

// Module Filtering Tests

func TestFactory_ExcludeModules(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "exclude")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id, ExcludeModules("test_b"))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_ExcludeClasses(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "exclude_class")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id, ExcludeClasses(luaapi.ClassIO))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_WithModule_AddsExtra(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "with_extra")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id, WithModule(factoryTestModuleB))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_WithFilter_CustomLogic(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "filter")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	filter := func(_ string, classes []string) (bool, error) {
		for _, c := range classes {
			if c == luaapi.ClassDeterministic {
				return true, nil
			}
		}
		return false, nil
	}

	factoryFn, err := pf.CreateFactory(id, WithFilter(filter))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

// AllowListed Mode Tests

func TestFactory_WithMode_AllowListed(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "allowlisted")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(id),
	)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_WithMode_AllowListed_FailsWithoutAllowed(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "allowlisted_fail")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	_, err := pf.CreateFactory(id, WithMode(code.AllowListed))
	if err == nil {
		t.Fatal("expected error when ID not in allowed list")
	}
}

func TestFactory_CombinedOptions(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "combined")
	addFactoryTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id,

		ExcludeClasses(luaapi.ClassNetwork),
		WithModule(factoryTestModuleA),
	)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

// Helper Function Tests

func TestFactory_Helpers(t *testing.T) {
	t.Run("toSet", func(t *testing.T) {
		set := toSet([]string{"a", "b", "c"})
		if len(set) != 3 {
			t.Errorf("expected 3 items, got %d", len(set))
		}
		if _, ok := set["a"]; !ok {
			t.Error("expected 'a' in set")
		}
		if _, ok := set["d"]; ok {
			t.Error("unexpected 'd' in set")
		}
	})

	t.Run("hasAnyClass", func(t *testing.T) {
		set := toSet([]string{luaapi.ClassIO, luaapi.ClassNetwork})

		if !hasAnyClass([]string{luaapi.ClassIO}, set) {
			t.Error("expected true for ClassIO")
		}
		if !hasAnyClass([]string{luaapi.ClassDeterministic, luaapi.ClassNetwork}, set) {
			t.Error("expected true when one class matches")
		}
		if hasAnyClass([]string{luaapi.ClassDeterministic}, set) {
			t.Error("expected false for ClassDeterministic")
		}
		if hasAnyClass(nil, set) {
			t.Error("expected false for nil classes")
		}
		if hasAnyClass([]string{}, set) {
			t.Error("expected false for empty classes")
		}
	})
}

// Transition Pattern Tests (from old manager patterns)

func TestFactory_Transition_FunctionManager(t *testing.T) {
	cm := setupFactoryCodeManager(t)
	id := registry.NewID("test", "function")
	addFactoryTestFunction(t, cm, id, `return function() return 42 end`)

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}

	proc2, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() second call failed: %v", err)
	}
	if proc2 == nil {
		t.Fatal("expected non-nil second process")
	}
}

func TestFactory_Transition_BytecodeManager(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	source := `return function() return "bytecode" end`
	proto, err := lua.CompileString(source, "bytecode_test")
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	id := registry.NewID("test", "bytecode_func")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.FunctionBytecode,
		Method: "main",
	}
	if err := cm.AddNodeWithProto(context.Background(), node, nil, proto); err != nil {
		t.Fatalf("AddNodeWithProto failed: %v", err)
	}

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_Transition_WorkflowManager(t *testing.T) {
	cm := setupFactoryCodeManager(t)
	id := registry.NewID("test", "workflow")
	addFactoryTestFunction(t, cm, id, `return function() return "workflow" end`)

	pf := NewProcessFactory(cm)

	factoryFn, err := pf.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(append(factoryWorkflowAllowedIDs, id)...),
	)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_Transition_WorkflowDeniedID(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	forbiddenID := registry.NewID("", "forbidden_lib")
	forbiddenNode := code.Node{
		ID:     forbiddenID,
		Kind:   luaapi.Library,
		Source: `return { value = "forbidden" }`,
	}
	if err := cm.AddNode(context.Background(), forbiddenNode, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	id := registry.NewID("test", "workflow_forbidden")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `local lib = require("forbidden_lib"); return function() return lib end`,
		Method: "main",
	}
	imports := []code.Import{{ID: forbiddenID, Alias: "forbidden_lib"}}
	if err := cm.AddNode(context.Background(), node, imports); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	pf := NewProcessFactory(cm)

	_, err := pf.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(append(factoryWorkflowAllowedIDs, id)...),
	)
	if err == nil {
		t.Fatal("expected error when using non-allowed dependency")
	}
}

// Consolidation Pattern Tests (source vs bytecode unified handling)

func TestFactory_Consolidation_SourcePath(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "source_func")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return "source" end`,
		Method: "main",
	}
	if err := cm.AddNode(context.Background(), node, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	factory := NewProcessFactory(cm)
	factoryFn, err := factory.CreateFactory(id, WithModule(factoryMockProcessModule))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_Consolidation_BytecodePath(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	source := `return function() return "bytecode" end`
	proto, err := lua.CompileString(source, "bytecode_func")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	id := registry.NewID("test", "bytecode_func")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.FunctionBytecode,
		Method: "main",
	}
	if err := cm.AddNodeWithProto(context.Background(), node, nil, proto); err != nil {
		t.Fatalf("AddNodeWithProto failed: %v", err)
	}

	factory := NewProcessFactory(cm)
	factoryFn, err := factory.CreateFactory(id, WithModule(factoryMockProcessModule))
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_Consolidation_UnifiedCreatePool(t *testing.T) {
	cm := setupFactoryCodeManager(t)
	factory := NewProcessFactory(cm)

	sourceID := registry.NewID("test", "source")
	bytecodeID := registry.NewID("test", "bytecode")

	_ = cm.AddNode(context.Background(), code.Node{
		ID:     sourceID,
		Kind:   luaapi.Function,
		Source: `return function() return 1 end`,
		Method: "main",
	}, nil)

	proto, _ := lua.CompileString(`return function() return 2 end`, "bc")
	_ = cm.AddNodeWithProto(context.Background(), code.Node{
		ID:     bytecodeID,
		Kind:   luaapi.FunctionBytecode,
		Method: "main",
	}, nil, proto)

	createPool := func(id registry.ID) error {
		factoryFn, err := factory.CreateFactory(id, WithModule(factoryMockProcessModule))
		if err != nil {
			return err
		}
		proc, err := factoryFn()
		if err != nil {
			return err
		}
		if proc == nil {
			return nil
		}
		return nil
	}

	if err := createPool(sourceID); err != nil {
		t.Fatalf("createPool(source) failed: %v", err)
	}
	if err := createPool(bytecodeID); err != nil {
		t.Fatalf("createPool(bytecode) failed: %v", err)
	}
}

func TestFactory_Consolidation_SandboxWithoutProcess(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "sandbox")
	_ = cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return "sandbox" end`,
		Method: "main",
	}, nil)

	factory := NewProcessFactory(cm)
	factoryFn, err := factory.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestFactory_Consolidation_WorkflowRestricted(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	id := registry.NewID("test", "workflow")
	_ = cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return "workflow" end`,
		Method: "main",
	}, nil)

	factory := NewProcessFactory(cm)

	factoryFn, err := factory.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(id),
	)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}
