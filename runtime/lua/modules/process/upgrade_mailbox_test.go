// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

type mailboxUpgradeFactory struct {
	next   procapi.Process
	create func()
}

func (f *mailboxUpgradeFactory) Create(registry.ID) (procapi.Process, *procapi.Meta, error) {
	if f.create != nil {
		f.create()
	}
	return f.next, &procapi.Meta{Method: "main"}, nil
}

func TestUpgradePreservesUnreadMailbox(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)
	frameCtx, runPID := tc.frameCtxPID(t)
	require.NoError(t, runtime.SetFrameID(frameCtx, registry.NewID("app", "receiver")))
	ctx, cancel := context.WithTimeout(frameCtx, 15*time.Second)
	defer cancel()

	ready := make(chan struct{}, 1)
	old := newProcessLeakProcess(t, `
		return { main = function()
			local data = process.listen("data")
			process.inbox()
			local upgrade = process.listen("upgrade")
			ready()
			upgrade:receive()
			assert(data:receive().index == 1)
			process.upgrade(nil, process.pid())
		end }
	`)
	old.State().SetGlobal("ready", lua.LGoFunc(func(*lua.LState) int {
		ready <- struct{}{}
		return 0
	}))
	next := newProcessLeakProcess(t, `
		return { main = function(original_pid)
			assert(process.pid() == original_pid, "PID changed")
			local data = process.listen("data")
			local inbox = process.inbox()
			for i = 2, 450 do
				if i % 2 == 1 then
					local value, ok = data:receive()
					assert(ok and value.index == i, "data out of order at " .. i)
				else
					local msg, ok = inbox:receive()
					assert(ok and msg:topic() == "other", "wrong inbox topic")
					assert(msg:from() == original_pid, "sender changed")
					assert(msg:data().index == i, "inbox out of order at " .. i)
				end
			end
			return "all messages received"
		end }
	`)
	messagePackage := func(first, last int) *relay.Package {
		pkg := relay.NewMessagePackage(runPID, runPID)
		for i := first; i <= last; i++ {
			topic := "data"
			if i%2 == 0 {
				topic = "other"
			}
			pkg.Messages = append(pkg.Messages, &relay.Message{
				Topic: topic, Payloads: payload.Payloads{payload.NewPayload(map[string]any{"index": i}, payload.Golang)},
			})
		}
		return pkg
	}
	procapi.WithFactory(ctx, &mailboxUpgradeFactory{next: next, create: func() {
		// These arrive after the old incarnation drained the first burst but
		// before the replacement runs. They must follow its retained backlog.
		if err := tc.node.Send(messagePackage(401, 450)); err != nil {
			panic(err)
		}
	}})
	go func() {
		select {
		case <-ready:
			pkg := messagePackage(1, 400)
			pkg.Messages = append(pkg.Messages, &relay.Message{Topic: "upgrade"})
			if err := tc.node.Send(pkg); err != nil {
				cancel()
			}
		case <-ctx.Done():
		}
	}()
	result, err := tc.scheduler.Execute(ctx, runPID, old, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, result.Error)
	require.Equal(t, "all messages received", result.Value.Data().(lua.LString).String())
}

func TestUpgradePreservesDeliveryToAnUnresumedCoroutine(t *testing.T) {
	tc := setupProcessLeakTest(t, 1)
	defer tc.Close(t)
	frameCtx, runPID := tc.frameCtxPID(t)
	ctx, cancel := context.WithTimeout(frameCtx, 10*time.Second)
	defer cancel()
	ready := make(chan struct{}, 1)
	old := newProcessLeakProcess(t, `
		return { main = function()
			local data = process.listen("data")
			local control = process.listen("control")
			coroutine.spawn(function()
				data:receive()
				error("old coroutine ran after upgrade was requested")
			end)
			ready()
			control:receive()
			process.upgrade("app:next")
		end }
	`)
	old.State().SetGlobal("ready", lua.LGoFunc(func(*lua.LState) int {
		ready <- struct{}{}
		return 0
	}))
	next := newProcessLeakProcess(t, `return { main = function()
		local data = process.listen("data")
		assert(data:receive() == "unread")
		return true
	end }`)
	procapi.WithFactory(ctx, &mailboxUpgradeFactory{next: next})
	go func() {
		select {
		case <-ready:
			// Wake the upgrading parent before the reader. The parent replaces
			// the old code before the reader can observe its delivery.
			if err := tc.node.Send(relay.NewMessagePackage(runPID, runPID,
				&relay.Message{Topic: "control"},
				&relay.Message{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(lua.LString("unread"), payload.Lua)}},
			)); err != nil {
				cancel()
			}
		case <-ctx.Done():
		}
	}()
	result, err := tc.scheduler.Execute(ctx, runPID, old, "main", nil)
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Equal(t, lua.LTrue, result.Value.Data())
}
