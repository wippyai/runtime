package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Test module definitions
var testModuleA = &luaapi.ModuleDef{
	Name:        "test_a",
	Description: "Test module A",
	Class:       []string{luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("a"))
		return tbl, nil
	},
}

var testModuleB = &luaapi.ModuleDef{
	Name:        "test_b",
	Description: "Test module B",
	Class:       []string{luaapi.ClassIO},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("b"))
		return tbl, nil
	},
}

var testModuleNetwork = &luaapi.ModuleDef{
	Name:        "test_network",
	Description: "Test network module",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("value", lua.LString("network"))
		return tbl, nil
	},
}

func setupTestCodeManager(t *testing.T) *code.Manager {
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

func addTestFunction(t *testing.T, cm *code.Manager, id registry.ID, source string) {
	t.Helper()
	node := code.Node{
		ID:     id,
		Kind:   luaapi.KindFunction,
		Source: source,
		Method: "main",
	}
	if err := cm.AddNode(context.Background(), node, nil); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}
}

func TestNewProcessFactory(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB}

	factory := NewProcessFactory(cm, nil, modules)
	if factory == nil {
		t.Fatal("expected non-nil factory")
	}
	if factory.code != cm {
		t.Error("code manager not set")
	}
	if len(factory.modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(factory.modules))
	}
}

func TestCreateFactory_Simple(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA}

	id := registry.NewID("test", "simple")
	addTestFunction(t, cm, id, `return function() return test_a.value end`)

	pf := NewProcessFactory(cm, nil, modules)
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

func TestCreateFactory_ReturnsReusableFactory(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA}

	id := registry.NewID("test", "factory")
	addTestFunction(t, cm, id, `return function() return 42 end`)

	pf := NewProcessFactory(cm, nil, modules)
	factoryFn, err := pf.CreateFactory(id)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}

	// Create multiple processes from same factory
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

func TestExcludeModules(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB}

	id := registry.NewID("test", "exclude")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Create with test_b excluded
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

func TestExcludeClasses(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB, testModuleNetwork}

	id := registry.NewID("test", "exclude_class")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Exclude IO class - should exclude test_b and test_network
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

func TestForbidModules_FailsOnForbidden(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB}

	id := registry.NewID("test", "forbid")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Forbid test_b - should fail because it's in defaults
	_, err := pf.CreateFactory(id, ForbidModules("test_b"))
	if err == nil {
		t.Fatal("expected error when forbidding default module")
	}
}

func TestForbidClasses_FailsOnForbidden(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleNetwork}

	id := registry.NewID("test", "forbid_class")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Forbid Network class - should fail because test_network is in defaults
	_, err := pf.CreateFactory(id, ForbidClasses(luaapi.ClassNetwork))
	if err == nil {
		t.Fatal("expected error when forbidding class present in defaults")
	}
}

func TestWithoutDefaultModule(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB}

	id := registry.NewID("test", "without_default")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Exclude test_a from defaults
	factoryFn, err := pf.CreateFactory(id, WithoutDefaultModule("test_a"))
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

func TestWithModule_AddsExtra(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA}

	id := registry.NewID("test", "with_extra")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Add test_b as extra
	factoryFn, err := pf.CreateFactory(id, WithModule(testModuleB))
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

func TestWithFilter_CustomLogic(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB, testModuleNetwork}

	id := registry.NewID("test", "filter")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Custom filter: only allow deterministic modules
	filter := func(name string, classes []string) (bool, error) {
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

func TestWithFilter_ReturnsError(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleNetwork}

	id := registry.NewID("test", "filter_error")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Custom filter that rejects network modules with error
	filter := func(name string, classes []string) (bool, error) {
		for _, c := range classes {
			if c == luaapi.ClassNetwork {
				return false, errors.New("network modules not allowed in this context")
			}
		}
		return true, nil
	}

	_, err := pf.CreateFactory(id, WithFilter(filter))
	if err == nil {
		t.Fatal("expected error from filter")
	}
}

func TestWithMode_AllowListed(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA}

	id := registry.NewID("test", "allowlisted")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Use AllowListed mode with the ID allowed
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

func TestWithMode_AllowListed_FailsWithoutAllowed(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA}

	id := registry.NewID("test", "allowlisted_fail")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Use AllowListed mode without allowing the ID - should fail
	_, err := pf.CreateFactory(id, WithMode(code.AllowListed))
	if err == nil {
		t.Fatal("expected error when ID not in allowed list")
	}
}

func TestCombinedOptions(t *testing.T) {
	cm := setupTestCodeManager(t)
	modules := []*luaapi.ModuleDef{testModuleA, testModuleB, testModuleNetwork}

	id := registry.NewID("test", "combined")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)

	// Combine multiple options
	factoryFn, err := pf.CreateFactory(id,
		WithoutDefaultModule("test_a"),
		ExcludeClasses(luaapi.ClassNetwork),
		WithModule(testModuleA), // Re-add as extra
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

func TestEmptyModulesList(t *testing.T) {
	cm := setupTestCodeManager(t)
	var modules []*luaapi.ModuleDef // Empty

	id := registry.NewID("test", "empty_modules")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	pf := NewProcessFactory(cm, nil, modules)
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

func TestHelperFunctions(t *testing.T) {
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
