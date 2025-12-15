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

// This test verifies the consolidation pattern for function managers:
// - Single manager handles both source and bytecode
// - Bytecode uses AddNodeWithProto so Compile() returns cached proto
// - ProcessFactory.CreateFactory() works for both

// Mock process module for testing
var mockProcessMod = &luaapi.ModuleDef{
	Name:        "process",
	Description: "Process module",
	Class:       []string{luaapi.ClassProcess},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 1)
		tbl.RawSetString("spawn", lua.LGoFunc(func(l *lua.LState) int { return 0 }))
		return tbl, nil
	},
}

// BaseModules - factory defaults WITHOUT process module
var BaseModules = []*luaapi.ModuleDef{
	testModuleA,
}

func TestConsolidation_SourcePath(t *testing.T) {
	log := zap.NewNop()
	cm, _ := code.NewCodeManager(log, nil, code.Config{})

	// Source-based: AddNode with source
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

	// ProcessFactory with base modules, add process per-call
	factory := NewProcessFactory(cm, nil, BaseModules)
	factoryFn, err := factory.CreateFactory(id,
		WithModule(mockProcessMod), // Add process module per-call
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

func TestConsolidation_BytecodePath(t *testing.T) {
	log := zap.NewNop()
	cm, _ := code.NewCodeManager(log, nil, code.Config{})

	// Simulate bytecode loading
	source := `return function() return "bytecode" end`
	proto, err := lua.CompileString(source, "bytecode_func")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// Bytecode: AddNodeWithProto (no source, proto injected)
	id := registry.NewID("test", "bytecode_func")
	node := code.Node{
		ID:     id,
		Kind:   luaapi.FunctionBytecode,
		Method: "main",
		// No Source field!
	}
	if err := cm.AddNodeWithProto(context.Background(), node, nil, proto); err != nil {
		t.Fatalf("AddNodeWithProto failed: %v", err)
	}

	// Same ProcessFactory call - doesn't know/care if bytecode
	factory := NewProcessFactory(cm, nil, BaseModules)
	factoryFn, err := factory.CreateFactory(id,
		WithModule(mockProcessMod),
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

func TestConsolidation_UnifiedCreatePool(t *testing.T) {
	log := zap.NewNop()
	cm, _ := code.NewCodeManager(log, nil, code.Config{})
	factory := NewProcessFactory(cm, nil, BaseModules)

	// Add both source and bytecode functions
	sourceID := registry.NewID("test", "source")
	bytecodeID := registry.NewID("test", "bytecode")

	// Source
	cm.AddNode(context.Background(), code.Node{
		ID:     sourceID,
		Kind:   luaapi.Function,
		Source: `return function() return 1 end`,
		Method: "main",
	}, nil)

	// Bytecode
	proto, _ := lua.CompileString(`return function() return 2 end`, "bc")
	cm.AddNodeWithProto(context.Background(), code.Node{
		ID:     bytecodeID,
		Kind:   luaapi.FunctionBytecode,
		Method: "main",
	}, nil, proto)

	// Unified createPool function - same code for both!
	createPool := func(id registry.ID) error {
		factoryFn, err := factory.CreateFactory(id,
			WithModule(mockProcessMod),
		)
		if err != nil {
			return err
		}

		// In real code: pass factoryFn to pool
		proc, err := factoryFn()
		if err != nil {
			return err
		}
		if proc == nil {
			return nil
		}
		return nil
	}

	// Both paths use same createPool
	if err := createPool(sourceID); err != nil {
		t.Fatalf("createPool(source) failed: %v", err)
	}
	if err := createPool(bytecodeID); err != nil {
		t.Fatalf("createPool(bytecode) failed: %v", err)
	}
}

func TestConsolidation_SandboxWithoutProcess(t *testing.T) {
	log := zap.NewNop()
	cm, _ := code.NewCodeManager(log, nil, code.Config{})

	id := registry.NewID("test", "sandbox")
	cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return "sandbox" end`,
		Method: "main",
	}, nil)

	// Sandbox: NO process module
	factory := NewProcessFactory(cm, nil, BaseModules)
	factoryFn, err := factory.CreateFactory(id)
	// No WithModule(processmod) - sandbox doesn't get process
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

func TestConsolidation_WorkflowRestricted(t *testing.T) {
	log := zap.NewNop()
	cm, _ := code.NewCodeManager(log, nil, code.Config{})

	id := registry.NewID("test", "workflow")
	cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Function,
		Source: `return function() return "workflow" end`,
		Method: "main",
	}, nil)

	factory := NewProcessFactory(cm, nil, BaseModules)

	// Workflow: AllowListed, no process module
	factoryFn, err := factory.CreateFactory(id,
		WithMode(code.AllowListed),
		WithAllowed(id), // Only allow the workflow itself
		// No WithModule(processmod) - workflows don't get it
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
