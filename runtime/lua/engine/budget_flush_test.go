// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// A message held back by a full channel is delivered to the receiver that
// parks on it even when that receive uses up the step's last tick: the
// process never reports idle while a queued message matches a parked receiver.
func TestProcessExhaustedBudgetDeliversQueuedMessage(t *testing.T) {
	for budget := int64(1); budget <= 40; budget++ {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			proto, err := lua.CompileString(`
				local ch = channel.new(1)
				subscribe("inbox", ch)
				local a = ch:receive()
				local b = ch:receive()
				return b
			`, "receive.lua")
			if err != nil {
				t.Fatal(err)
			}
			proc := mustNewProcess(t, WithProto(proto), WithProcessExecutionBudgets(luaapi.ExecutionBudgets{TickBudget: budget, TickBudgetSet: true}))
			proc.EnablePreemption()
			t.Cleanup(proc.Close)
			ctx, _ := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
			ctx = payload.WithTranscoder(ctx, createInboxTestTranscoder())
			if err := proc.Init(ctx, "", nil); err != nil {
				t.Fatal(err)
			}
			LoadModuleDef(proc.State(), ChannelModule)
			loadPubSubGlobals(proc.State())

			var output process.StepOutput
			events := []process.Event{{
				Type: process.EventMessage,
				Data: &relay.Package{Messages: []*relay.Message{
					{Topic: "inbox", Payloads: payload.Payloads{payload.NewPayload(lua.LString("first"), payload.Lua)}},
					{Topic: "inbox", Payloads: payload.Payloads{payload.NewPayload(lua.LString("second"), payload.Lua)}},
				}},
			}}
			for i := 0; i < 200; i++ {
				output.Reset()
				if err := proc.Step(events, &output); err != nil {
					t.Fatal(err)
				}
				events = nil
				switch output.Status() {
				case process.StepDone:
					return
				case process.StepIdle:
					t.Fatalf("step %d: idle with a queued message for the parked receiver", i)
				}
			}
			t.Fatal("process did not complete")
		})
	}
}
