package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	lua "github.com/yuin/gopher-lua"
)

func TestUpgradeRequest_LuaValue(t *testing.T) {
	req := &UpgradeRequest{
		Source: registry.NewID("test", "handler"),
		Input:  payload.Payloads{payload.New("arg1")},
	}

	// Verify it implements lua.LValue interface
	var lv lua.LValue = req
	if lv.String() != "<upgrade_request>" {
		t.Errorf("Expected String() = <upgrade_request>, got %s", lv.String())
	}
	if lv.Type() != lua.LTUserData {
		t.Errorf("Expected Type() = LTUserData, got %v", lv.Type())
	}
}

func TestProcess_UpgradeRequest_Detection(t *testing.T) {
	// Script that yields an UpgradeRequest
	script := `
		return function()
			coroutine.yield(upgrade_request)
		end
	`

	// Create upgrade request to yield
	upgradeReq := &UpgradeRequest{
		Source: registry.NewID("test", "new_handler"),
		Input:  nil,
	}

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(func(l *lua.LState) {
			l.SetGlobal("upgrade_request", upgradeReq)
		}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// Verify StepUpgrade status
	if output.Status() != process.StepUpgrade {
		t.Errorf("Expected StepUpgrade status, got %v", output.Status())
	}

	// Verify upgrade request is set
	req := output.Upgrade()
	if req == nil {
		t.Fatal("Expected upgrade request, got nil")
	}
	if req.Source.String() != "test:new_handler" {
		t.Errorf("Expected source test:new_handler, got %s", req.Source.String())
	}
}

func TestProcess_UpgradeRequest_EmptySource(t *testing.T) {
	// Script that yields an UpgradeRequest with empty source (restart same)
	script := `
		return function()
			coroutine.yield(upgrade_request)
		end
	`

	// Create upgrade request with empty source
	upgradeReq := &UpgradeRequest{
		Source: registry.ID{}, // empty
		Input:  payload.Payloads{payload.New("restart_arg")},
	}

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(func(l *lua.LState) {
			l.SetGlobal("upgrade_request", upgradeReq)
		}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepUpgrade {
		t.Errorf("Expected StepUpgrade status, got %v", output.Status())
	}

	req := output.Upgrade()
	if req == nil {
		t.Fatal("Expected upgrade request, got nil")
	}
	if req.Source.Name != "" {
		t.Errorf("Expected empty source, got %s", req.Source.String())
	}
	if len(req.Input) != 1 {
		t.Errorf("Expected 1 input payload, got %d", len(req.Input))
	}
}

func TestProcess_UpgradeRequest_ClearsExecution(t *testing.T) {
	// Script with coroutine that yields upgrade
	script := `
		return function()
			coroutine.yield(upgrade_request)
			return "should not reach"
		end
	`

	upgradeReq := &UpgradeRequest{
		Source: registry.NewID("test", "handler"),
	}

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(func(l *lua.LState) {
			l.SetGlobal("upgrade_request", upgradeReq)
		}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	_ = proc.Step(nil, &output)

	// Verify execution was cleared
	if len(proc.threads) != 0 {
		t.Errorf("Expected threads to be cleared, got %d", len(proc.threads))
	}
	if proc.mainTask != nil {
		t.Error("Expected mainTask to be nil")
	}
}
