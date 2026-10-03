// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

// The module bootstrap does not use globals the guest can change.
func TestProcessModuleBootstrapIgnoresShadowedGlobals(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`
		select = nil
		pcall = nil
		unpack = nil
		table = nil
		return { main = function(a, b) return a + b end }
	`, "module.lua"))
	t.Cleanup(proc.Close)
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	args := payload.Payloads{&testPayload{val: lua.LNumber(10)}, &testPayload{val: lua.LNumber(20)}}
	if err := proc.Init(ctx, "main", args); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if v, _ := output.Result().Data().(lua.LValue); lua.LVAsNumber(v) != 30 {
		t.Fatalf("expected 30, got %v", output.Result().Data())
	}
}
