// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// TestOutdated_SetGetOptionsRoundTrip runs a real process that sets and reads
// the upgradable option and checks non-boolean rejection.
func TestOutdated_SetGetOptionsRoundTrip(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)

	script := `
		local ok, err = process.set_options({ upgradable = true })
		if not ok then error("set_options: " .. tostring(err)) end
		local opts = process.get_options()
		if opts.upgradable ~= true then error("upgradable not set") end
		if opts.trap_links ~= false then error("trap_links should default to false") end
		local bad = process.set_options({ upgradable = "nope" })
		if bad then error("expected non-boolean upgradable to be rejected") end
		return true
	`

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newProcessLeakProcess(t, script)

	ctx, cancel := context.WithTimeout(frameCtx, 10*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
}

// TestOutdated_UpgradableProcessReceivesEvent runs a real upgradable process
// that waits on events() and asserts it receives OUTDATED with the correct
// sources after the scheduler notifies its source node.
func TestOutdated_UpgradableProcessReceivesEvent(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)

	src := registry.NewID("app", "worker")

	script := `
		local ok, err = process.set_options({ upgradable = true })
		if not ok then error("set_options: " .. tostring(err)) end
		local ev = process.events()
		local msg, rok = ev:receive()
		if not rok then error("events channel closed") end
		if msg.kind ~= process.event.OUTDATED then error("wrong kind: " .. tostring(msg.kind)) end
		if msg.sources[1] ~= "app:worker" then error("wrong source: " .. tostring(msg.sources[1])) end
		return true
	`

	frameCtx, runPID := tc.frameCtxPID(t)
	require.NoError(t, runtime.SetFrameID(frameCtx, src))
	proc := newProcessLeakProcess(t, script)

	// Notify the source once the process is running; the engine retains and
	// redelivers if the events() subscription is not yet live.
	// Notify once the process is established and parked on events(), mirroring a
	// long-running process being notified when its code is updated.
	go func() {
		deadline := stdtime.Now().Add(5 * stdtime.Second)
		for stdtime.Now().Before(deadline) {
			for _, pinfo := range tc.scheduler.ListProcesses() {
				if pinfo.Source.Equal(src) && (pinfo.State == "idle" || pinfo.State == "blocked") {
					tc.scheduler.SendOutdated(map[registry.ID]bool{src: true})
					return
				}
			}
			stdtime.Sleep(2 * stdtime.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(frameCtx, 10*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
}

// TestOutdated_DeliveredBeforeSubscribe proves an OUTDATED sent while the target
// is established but has not yet subscribed to events() is retained and
// delivered once the subscription appears. The process first parks on a control
// listener; OUTDATED is sent then; only after a control message does it
// subscribe to events() and receive the retained event.
func TestOutdated_DeliveredBeforeSubscribe(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)

	src := registry.NewID("app", "worker")

	script := `
		local ok, err = process.set_options({ upgradable = true })
		if not ok then error("set_options: " .. tostring(err)) end
		-- Establish, but do NOT subscribe to events() yet.
		local ctrl = process.listen("control")
		ctrl:receive()
		-- Now subscribe: the earlier OUTDATED must be delivered here.
		local ev = process.events()
		local msg, rok = ev:receive()
		if not rok then error("events channel closed") end
		if msg.kind ~= process.event.OUTDATED then error("wrong kind: " .. tostring(msg.kind)) end
		if msg.sources[1] ~= "app:worker" then error("wrong source: " .. tostring(msg.sources[1])) end
		return true
	`

	frameCtx, runPID := tc.frameCtxPID(t)
	require.NoError(t, runtime.SetFrameID(frameCtx, src))
	proc := newProcessLeakProcess(t, script)

	go func() {
		deadline := stdtime.Now().Add(5 * stdtime.Second)
		sent := false
		for stdtime.Now().Before(deadline) {
			for _, pinfo := range tc.scheduler.ListProcesses() {
				if !pinfo.Source.Equal(src) || (pinfo.State != "idle" && pinfo.State != "blocked") {
					continue
				}
				if !sent {
					// Parked on control, no events() subscription yet.
					tc.scheduler.SendOutdated(map[registry.ID]bool{src: true})
					sent = true
					stdtime.Sleep(30 * stdtime.Millisecond)
					continue
				}
				// Release the control listener so the process subscribes to events().
				_ = tc.scheduler.Send(relay.NewPackage(pid.PID{}, runPID, "control", payload.NewString("go")))
				return
			}
			stdtime.Sleep(2 * stdtime.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(frameCtx, 10*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
}
