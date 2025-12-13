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

// This file demonstrates the transition from current manager patterns to ProcessFactory.
// These tests verify that ProcessFactory can handle all existing use cases.

// Mock process module (similar to runtime/lua/modules/process)
var mockProcessModule = &luaapi.ModuleDef{
	Name:        "process",
	Description: "Process management module",
	Class:       []string{luaapi.ClassProcess},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 2)
		tbl.RawSetString("spawn", lua.LGoFunc(func(l *lua.LState) int { return 0 }))
		tbl.RawSetString("send", lua.LGoFunc(func(l *lua.LState) int { return 0 }))
		return tbl, nil
	},
}

// DefaultModulesWithProcess simulates CoreModules with process module added
var DefaultModulesWithProcess = []*luaapi.ModuleDef{
	testModuleA,
	testModuleB,
	mockProcessModule,
}

// WorkflowAllowedModules simulates the allowed modules for workflows
var workflowAllowedIDs = []registry.ID{
	{Name: "json"},
	{Name: "base64"},
	{Name: "payload"},
	{Name: "workflow"},
	{Name: "channel"},
}

// TestTransition_FunctionManager tests the function manager pattern
// Current: Compile() + createProcess() with CoreBinders + processmod.BindGlobal + deps
// New: CreateFactory() with default modules including process
func TestTransition_FunctionManager(t *testing.T) {
	cm := setupTestCodeManager(t)
	id := registry.NewID("test", "function")
	addTestFunction(t, cm, id, `return function() return 42 end`)

	// With ProcessFactory - process module is in defaults
	pf := NewProcessFactory(cm, nil, DefaultModulesWithProcess)
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

	// Factory should be reusable
	proc2, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() second call failed: %v", err)
	}
	if proc2 == nil {
		t.Fatal("expected non-nil second process")
	}
}

// TestTransition_BytecodeFunctionManager tests the bytecode function manager pattern
// Current: Load bytecode -> AddNodeWithProto() -> Compile() -> createBytecodeProcess(proto, compiled)
// New: Load bytecode -> AddNodeWithProto() -> CreateFactory() (proto comes from Compile)
func TestTransition_BytecodeFunctionManager(t *testing.T) {
	log := zap.NewNop()
	cm, err := code.NewCodeManager(log, nil, code.Config{
		Modules:        nil,
		ProtoCacheSize: 100,
		MainCacheSize:  100,
	})
	if err != nil {
		t.Fatalf("failed to create code manager: %v", err)
	}

	// Simulate bytecode loading: compile source to get proto
	source := `return function() return "bytecode" end`
	proto, err := lua.CompileString(source, "bytecode_test")
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Add node with proto (simulates bytecode injection)
	id := registry.NewID("test", "bytecode_func")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.KindFunctionBytecode,
		Method: "main",
	}
	if err := cm.AddNodeWithProto(context.Background(), node, nil, proto); err != nil {
		t.Fatalf("AddNodeWithProto failed: %v", err)
	}

	// With ProcessFactory - should use the injected proto
	pf := NewProcessFactory(cm, nil, DefaultModulesWithProcess)
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

// TestTransition_ProcessManager tests the process manager pattern
// Same as function manager
func TestTransition_ProcessManager(t *testing.T) {
	cm := setupTestCodeManager(t)
	id := registry.NewID("test", "process_entry")
	addTestFunction(t, cm, id, `return function() return "process" end`)

	pf := NewProcessFactory(cm, nil, DefaultModulesWithProcess)
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

// TestTransition_WorkflowManager tests the workflow manager pattern
// Current: Compile(AllowListed + specific allowed) + createProcess() without processmod
// New: CreateFactory() with AllowListed + allowed IDs + WithoutDefaultModule("process")
func TestTransition_WorkflowManager(t *testing.T) {
	cm := setupTestCodeManager(t)
	id := registry.NewID("test", "workflow")
	addTestFunction(t, cm, id, `return function() return "workflow" end`)

	pf := NewProcessFactory(cm, nil, DefaultModulesWithProcess)

	// Workflow pattern: AllowListed mode, specific allowed IDs, no process module
	factoryFn, err := pf.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(append(workflowAllowedIDs, id)...), // Include the workflow ID itself
		WithoutDefaultModule("process"),                // Workflows don't get process module
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

// TestTransition_WorkflowManager_FailsWithDeniedID tests that workflows
// fail at compile time when using denied IDs
func TestTransition_WorkflowManager_FailsWithDeniedID(t *testing.T) {
	cm := setupTestCodeManager(t)

	// Add a "forbidden" library
	forbiddenID := registry.NewID("", "forbidden_lib")
	forbiddenNode := code.Node{
		ID:     forbiddenID,
		Kind:   luaapi.KindLibrary,
		Source: `return { value = "forbidden" }`,
	}
	if err := cm.AddNode(context.Background(), forbiddenNode, nil); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Add workflow that imports the forbidden lib
	id := registry.NewID("test", "workflow_forbidden")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.KindFunction,
		Source: `local lib = require("forbidden_lib"); return function() return lib end`,
		Method: "main",
	}
	imports := []code.Import{{ID: forbiddenID, Alias: "forbidden_lib"}}
	if err := cm.AddNode(context.Background(), node, imports); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	pf := NewProcessFactory(cm, nil, DefaultModulesWithProcess)

	// Should fail because forbidden_lib is not in allowed list
	_, err := pf.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(append(workflowAllowedIDs, id)...), // forbidden_lib not included
		WithoutDefaultModule("process"),
	)
	if err == nil {
		t.Fatal("expected error when using non-allowed dependency in workflow")
	}
}

// TestTransition_RuntimeFiltering tests runtime-level filtering
// (separate from compile-time filtering)
func TestTransition_RuntimeFiltering(t *testing.T) {
	cm := setupTestCodeManager(t)
	id := registry.NewID("test", "runtime_filter")
	addTestFunction(t, cm, id, `return function() return 1 end`)

	// Factory with network module in defaults
	modulesWithNetwork := []*luaapi.ModuleDef{
		testModuleA,
		testModuleNetwork,
		mockProcessModule,
	}
	pf := NewProcessFactory(cm, nil, modulesWithNetwork)

	// Runtime forbid: fail if network class is in defaults
	_, err := pf.CreateFactory(id,
		ForbidClasses(luaapi.ClassNetwork),
	)
	if err == nil {
		t.Fatal("expected error when forbidding network class at runtime")
	}

	// Runtime exclude: silently skip network module
	factoryFn, err := pf.CreateFactory(id,
		ExcludeClasses(luaapi.ClassNetwork),
	)
	if err != nil {
		t.Fatalf("CreateFactory with ExcludeClasses failed: %v", err)
	}

	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}
