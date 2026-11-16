// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func newLifecycleUpgradeProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "lifecycle_upgrade.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(engine.WithProto(proto), engine.WithModuleBinder(func(l *lua.LState) error {
		engine.LoadModuleDef(l, engine.ChannelModule)
		bindProcess(l)
		l.SetGlobal("upgrade_request", &engine.UpgradeRequest{})
		l.SetGlobal("upgrade_gate", l.NewFunction(func(l *lua.LState) int {
			l.Push(&engine.SubscribeRequest{Topic: "upgrade-gate"})
			return -1
		}))
		return nil
	}))
	require.NoError(t, err)
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	t.Cleanup(func() { proc.Close(); ctxapi.ReleaseFrameContext(frame) })
	require.NoError(t, proc.Init(ctx, "", nil))
	return proc
}

func TestLifecycleEventsSurviveUpgradeInOrderExactlyOnce(t *testing.T) {
	old := newLifecycleUpgradeProcess(t, `
		local events = process.events()
		assert(events:receive().reason == "consumed")
		upgrade_gate():receive()
		coroutine.yield(upgrade_request)
	`)
	var out processapi.StepOutput
	require.NoError(t, old.Step(nil, &out))
	require.Equal(t, processapi.StepIdle, out.Status())
	message := func(topic string, value any) processapi.Event {
		return processapi.Event{Type: processapi.EventMessage, Data: relay.NewPackage(
			pid.PID{Host: "test", UniqID: "sender"}, pid.PID{Host: "test", UniqID: "owner"}, topic, payload.New(value))}
	}
	// The owner consumes one event, then waits outside its lifecycle channel.
	// CANCEL and EXIT remain unread when the gate allows the upgrade to run.
	require.NoError(t, old.Step([]processapi.Event{
		message(topology.TopicEvents, &topology.CancelEvent{Kind: topology.Cancel, Reason: "consumed"}),
		message(topology.TopicEvents, &topology.CancelEvent{Kind: topology.Cancel, Reason: "pending"}),
		message(topology.TopicEvents, &topology.ExitEvent{Kind: topology.Exit, From: pid.PID{Host: "test", UniqID: "child"}}),
		message("upgrade-gate", "continue"),
	}, &out))
	require.Equal(t, processapi.StepUpgrade, out.Status())

	next := newLifecycleUpgradeProcess(t, `
		local events = process.events()
		processed = 0
		local cancelled = events:receive()
		assert(cancelled.kind == process.event.CANCEL and cancelled.reason == "pending")
		local exited = events:receive()
		assert(exited.kind == process.event.EXIT)
		processed = 2
		local live = events:receive()
		assert(live.kind == process.event.CANCEL and live.reason == "live")
		processed = 3
		events:receive()
	`)
	require.NoError(t, old.TransferUpgradeState(next))
	require.NoError(t, next.Step(nil, &out))
	require.Equal(t, processapi.StepIdle, out.Status())
	require.Equal(t, "2", next.State().GetGlobal("processed").String(), "unread lifecycle events must survive the upgrade")

	// Consumed or duplicated events would fail the assertions before this live
	// control message; the replacement must also keep receiving new events.
	require.NoError(t, next.Step([]processapi.Event{
		message(topology.TopicEvents, &topology.CancelEvent{Kind: topology.Cancel, Reason: "live"}),
	}, &out))
	require.Equal(t, processapi.StepIdle, out.Status())
	require.Equal(t, "3", next.State().GetGlobal("processed").String())
}
