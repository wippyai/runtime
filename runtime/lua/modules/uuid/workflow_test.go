package uuid

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/workflow"
	luaworkflow "github.com/wippyai/runtime/runtime/lua/workflow"
)

func TestUUIDV4_NormalMode(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v4()
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v4 normal mode test failed: %v", err)
	}
}

func TestUUIDV4_DeterministicMode_Yields(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := workflow.SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	// Directly call the Go function and check return value
	ret := uuidV4(l)

	// ret == -1 means yield
	if ret != -1 {
		t.Fatalf("expected -1 (yield), got %d", ret)
	}

	top := l.GetTop()
	if top != 1 {
		t.Fatalf("expected 1 value on stack, got %d", top)
	}

	val := l.Get(1)
	yield, ok := val.(*luaworkflow.Yield)
	if !ok {
		t.Fatalf("expected workflow.Yield, got %T", val)
	}

	if yield.Cmd == nil {
		t.Fatal("yield.Cmd is nil")
	}

	if yield.Cmd.Fn == nil {
		t.Fatal("yield.Cmd.Fn is nil")
	}

	result, err := yield.Cmd.Fn()
	if err != nil {
		t.Fatalf("closure execution failed: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if len(str) != 36 {
		t.Errorf("expected UUID length 36, got %d", len(str))
	}
}

func TestUUIDV1_DeterministicMode_Yields(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := workflow.SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ret := uuidV1(l)

	if ret != -1 {
		t.Fatalf("expected -1 (yield), got %d", ret)
	}

	val := l.Get(1)
	yield, ok := val.(*luaworkflow.Yield)
	if !ok {
		t.Fatalf("expected workflow.Yield, got %T", val)
	}

	result, err := yield.Cmd.Fn()
	if err != nil {
		t.Fatalf("closure execution failed: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if len(str) != 36 {
		t.Errorf("expected UUID length 36, got %d", len(str))
	}
}

func TestUUIDV7_DeterministicMode_Yields(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := workflow.SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ret := uuidV7(l)

	if ret != -1 {
		t.Fatalf("expected -1 (yield), got %d", ret)
	}

	val := l.Get(1)
	yield, ok := val.(*luaworkflow.Yield)
	if !ok {
		t.Fatalf("expected workflow.Yield, got %T", val)
	}

	result, err := yield.Cmd.Fn()
	if err != nil {
		t.Fatalf("closure execution failed: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if len(str) != 36 {
		t.Errorf("expected UUID length 36, got %d", len(str))
	}
}

func TestUUIDV3_DeterministicMode_NoYield(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := workflow.SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		local id, err = uuid.v3(ns, "test")
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v3 should not yield in deterministic mode: %v", err)
	}
}

func TestUUIDV5_DeterministicMode_NoYield(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := workflow.SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		local id, err = uuid.v5(ns, "test")
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v5 should not yield in deterministic mode: %v", err)
	}
}
