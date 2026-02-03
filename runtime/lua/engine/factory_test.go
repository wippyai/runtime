package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
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

// Module Aliasing Tests

func TestFactory_ModuleAliasing_GoModule(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	// Register a Go module
	testMod := &luaapi.ModuleDef{
		Name:        "original_name",
		Description: "Test module for aliasing",
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			tbl := lua.CreateTable(0, 1)
			tbl.RawSetString("marker", lua.LString("from_original_name"))
			return tbl, nil
		},
	}

	modNode := code.Node{
		ID:     registry.NewID("", "original_name"),
		Kind:   luaapi.ModuleKind,
		Module: testMod,
	}
	if err := cm.AddNode(context.Background(), modNode, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Create a function that imports the module with an alias
	id := registry.NewID("test", "alias_test")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return aliased_module.marker end`,
		Method: "main",
	}
	imports := []code.Import{{ID: registry.NewID("", "original_name"), Alias: "aliased_module"}}
	if err := cm.AddNode(context.Background(), node, imports); err != nil {
		t.Fatalf("AddNode failed: %v", err)
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

	// Verify the alias is set correctly in the Lua state
	luaProc := proc.(*Process)
	state := luaProc.State()

	// Check aliased_module global exists
	aliasedMod := state.GetGlobal("aliased_module")
	if aliasedMod == lua.LNil {
		t.Fatal("aliased_module global not found")
	}

	// Verify it's a table with the expected marker
	tbl, ok := aliasedMod.(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %T", aliasedMod)
	}

	marker := tbl.RawGetString("marker")
	if marker.String() != "from_original_name" {
		t.Errorf("expected 'from_original_name', got '%s'", marker.String())
	}

	// Verify original_name is NOT set as global
	originalMod := state.GetGlobal("original_name")
	if originalMod != lua.LNil {
		t.Error("original_name should not be a global when aliased")
	}
}

func TestFactory_ModuleAliasing_LuaLibrary(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	// Create a Lua library
	libID := registry.NewID("", "my_utils")
	libNode := code.Node{
		ID:     libID,
		Kind:   luaapi.Library,
		Source: `return { marker = "from_utils" }`,
	}
	if err := cm.AddNode(context.Background(), libNode, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Create a function that imports the library with an alias
	id := registry.NewID("test", "lib_alias_test")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return utils.marker end`,
		Method: "main",
	}
	imports := []code.Import{{ID: libID, Alias: "utils"}}
	if err := cm.AddNode(context.Background(), node, imports); err != nil {
		t.Fatalf("AddNode failed: %v", err)
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

	// Verify the alias is set correctly in the Lua state
	luaProc := proc.(*Process)
	state := luaProc.State()

	// Check utils global exists
	utilsMod := state.GetGlobal("utils")
	if utilsMod == lua.LNil {
		t.Fatal("utils global not found")
	}

	// Verify it's a table with the expected marker
	tbl, ok := utilsMod.(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %T", utilsMod)
	}

	marker := tbl.RawGetString("marker")
	if marker.String() != "from_utils" {
		t.Errorf("expected 'from_utils', got '%s'", marker.String())
	}

	// Verify my_utils is NOT set as global
	originalMod := state.GetGlobal("my_utils")
	if originalMod != lua.LNil {
		t.Error("my_utils should not be a global when aliased")
	}
}

func TestFactory_ModuleAliasing_MultipleAliases(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	// Create two modules
	mod1 := &luaapi.ModuleDef{
		Name:        "module_one",
		Description: "First module",
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			tbl := lua.CreateTable(0, 1)
			tbl.RawSetString("id", lua.LString("one"))
			return tbl, nil
		},
	}

	mod2 := &luaapi.ModuleDef{
		Name:        "module_two",
		Description: "Second module",
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			tbl := lua.CreateTable(0, 1)
			tbl.RawSetString("id", lua.LString("two"))
			return tbl, nil
		},
	}

	if err := cm.AddNode(context.Background(), code.Node{
		ID:     registry.NewID("", "module_one"),
		Kind:   luaapi.ModuleKind,
		Module: mod1,
	}, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	if err := cm.AddNode(context.Background(), code.Node{
		ID:     registry.NewID("", "module_two"),
		Kind:   luaapi.ModuleKind,
		Module: mod2,
	}, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Function that uses both modules with different aliases
	id := registry.NewID("test", "multi_alias")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return first.id .. "+" .. second.id end`,
		Method: "main",
	}
	imports := []code.Import{
		{ID: registry.NewID("", "module_one"), Alias: "first"},
		{ID: registry.NewID("", "module_two"), Alias: "second"},
	}
	if err := cm.AddNode(context.Background(), node, imports); err != nil {
		t.Fatalf("AddNode failed: %v", err)
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

	luaProc := proc.(*Process)
	state := luaProc.State()

	// Verify first alias
	firstMod := state.GetGlobal("first")
	if firstMod == lua.LNil {
		t.Fatal("first global not found")
	}
	if tbl, ok := firstMod.(*lua.LTable); ok {
		if tbl.RawGetString("id").String() != "one" {
			t.Errorf("expected 'one', got '%s'", tbl.RawGetString("id").String())
		}
	} else {
		t.Fatalf("expected table, got %T", firstMod)
	}

	// Verify second alias
	secondMod := state.GetGlobal("second")
	if secondMod == lua.LNil {
		t.Fatal("second global not found")
	}
	if tbl, ok := secondMod.(*lua.LTable); ok {
		if tbl.RawGetString("id").String() != "two" {
			t.Errorf("expected 'two', got '%s'", tbl.RawGetString("id").String())
		}
	} else {
		t.Fatalf("expected table, got %T", secondMod)
	}

	// Verify original names are NOT set
	if state.GetGlobal("module_one") != lua.LNil {
		t.Error("module_one should not be a global when aliased")
	}
	if state.GetGlobal("module_two") != lua.LNil {
		t.Error("module_two should not be a global when aliased")
	}
}

func TestFactory_ModuleAliasing_SameModuleDifferentAliases(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	// Create one module
	testMod := &luaapi.ModuleDef{
		Name:        "shared_mod",
		Description: "Shared module",
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			tbl := lua.CreateTable(0, 1)
			tbl.RawSetString("name", lua.LString("shared"))
			return tbl, nil
		},
	}

	if err := cm.AddNode(context.Background(), code.Node{
		ID:     registry.NewID("", "shared_mod"),
		Kind:   luaapi.ModuleKind,
		Module: testMod,
	}, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Two functions importing the same module with different aliases
	id1 := registry.NewID("test", "alias_a")
	node1 := code.Node{
		ID:     id1,
		Kind:   luaapi.Function,
		Source: `return function() return alpha.name end`,
		Method: "main",
	}
	imports1 := []code.Import{{ID: registry.NewID("", "shared_mod"), Alias: "alpha"}}
	if err := cm.AddNode(context.Background(), node1, imports1); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	id2 := registry.NewID("test", "alias_b")
	node2 := code.Node{
		ID:     id2,
		Kind:   luaapi.Function,
		Source: `return function() return beta.name end`,
		Method: "main",
	}
	imports2 := []code.Import{{ID: registry.NewID("", "shared_mod"), Alias: "beta"}}
	if err := cm.AddNode(context.Background(), node2, imports2); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	pf := NewProcessFactory(cm)

	// Test first alias
	factory1, err := pf.CreateFactory(id1)
	if err != nil {
		t.Fatalf("CreateFactory for id1 failed: %v", err)
	}
	proc1, err := factory1()
	if err != nil {
		t.Fatalf("factory1() failed: %v", err)
	}
	state1 := proc1.(*Process).State()

	// Verify alpha is set but not shared_mod
	alphaMod := state1.GetGlobal("alpha")
	if alphaMod == lua.LNil {
		t.Fatal("alpha global not found in proc1")
	}
	if tbl, ok := alphaMod.(*lua.LTable); ok {
		if tbl.RawGetString("name").String() != "shared" {
			t.Errorf("proc1: expected 'shared', got '%s'", tbl.RawGetString("name").String())
		}
	}
	if state1.GetGlobal("shared_mod") != lua.LNil {
		t.Error("shared_mod should not be a global in proc1")
	}
	if state1.GetGlobal("beta") != lua.LNil {
		t.Error("beta should not be a global in proc1")
	}

	// Test second alias
	factory2, err := pf.CreateFactory(id2)
	if err != nil {
		t.Fatalf("CreateFactory for id2 failed: %v", err)
	}
	proc2, err := factory2()
	if err != nil {
		t.Fatalf("factory2() failed: %v", err)
	}
	state2 := proc2.(*Process).State()

	// Verify beta is set but not shared_mod
	betaMod := state2.GetGlobal("beta")
	if betaMod == lua.LNil {
		t.Fatal("beta global not found in proc2")
	}
	if tbl, ok := betaMod.(*lua.LTable); ok {
		if tbl.RawGetString("name").String() != "shared" {
			t.Errorf("proc2: expected 'shared', got '%s'", tbl.RawGetString("name").String())
		}
	}
	if state2.GetGlobal("shared_mod") != lua.LNil {
		t.Error("shared_mod should not be a global in proc2")
	}
	if state2.GetGlobal("alpha") != lua.LNil {
		t.Error("alpha should not be a global in proc2")
	}
}
