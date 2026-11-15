// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	sysprocess "github.com/wippyai/runtime/system/process"
)

func TestTerminateAfterConsumedOutdated(t *testing.T) {
	for _, outdated := range []bool{false, true} {
		name := "current"
		if outdated {
			name = "outdated"
		}
		t.Run(name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, result *runtime.Result) {
				completed <- result
			}})
			p, err := engine.NewProcess(
				engine.WithScript(`local ev = events(); local msg = ev:receive(); assert(msg.kind == "pid.outdated"); channel.new(0):receive()`, "retirement.lua"),
				engine.WithModuleBinder(func(l *lua.LState) error {
					engine.LoadModuleDef(l, engine.ChannelModule)
					l.SetGlobal("events", l.NewFunction(func(l *lua.LState) int {
						l.Push(&engine.SubscribeRequest{Topic: topology.TopicEvents, BufSize: 1})
						return -1
					}))
					return nil
				}),
			)
			require.NoError(t, err)
			ctx, frame := ctxapi.OpenFrameContext(context.Background())
			defer ctxapi.ReleaseFrameContext(frame)
			source := registry.NewID("app", "worker")
			require.NoError(t, runtime.SetFrameID(ctx, source))
			id := pidapi.PID{UniqID: "retiring"}
			proc, err := sched.Submit(ctx, id, p, "", nil)
			require.NoError(t, err)
			p.SetUpgradable(true)
			worker := sched.workerSnapshot()[0]
			worker.executeOne(proc)
			require.Equal(t, StateIdle, ProcessState(proc.state.Load())&stateMask)
			if outdated {
				sched.SendOutdated(map[registry.ID]bool{source: true})
				worker.executeOne(proc)
				require.Equal(t, StateIdle, ProcessState(proc.state.Load())&stateMask)
				require.False(t, proc.queue.HasEvents())
			}
			require.NoError(t, sched.Terminate(id))
			// Drive the cancelled incarnation without letting another Step run.
			proc.casState(StateIdle, StateReady)
			worker.executeOne(proc)
			select {
			case result := <-completed:
				require.ErrorIs(t, result.Error, sysprocess.ErrTerminated)
				require.Equal(t, outdated, result.Outdated)
			default:
				t.Fatal("terminated process does not report completion")
			}
		})
	}
}

func TestCompletionInspectsNilQueueEntries(t *testing.T) {
	for _, tc := range []struct {
		pkg      *relay.Package
		name     string
		outdated bool
	}{
		{name: "nil package"},
		{name: "nil message", pkg: relay.NewMessagePackage(pidapi.PID{}, pidapi.PID{}, nil)},
		{name: "nil payload", pkg: relay.NewPackage(pidapi.PID{}, pidapi.PID{}, topology.TopicEvents, nil)},
		{name: "mixed payloads", pkg: relay.NewPackage(pidapi.PID{}, pidapi.PID{}, topology.TopicEvents,
			nil, payload.New(&topology.OutdatedEvent{}), nil), outdated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, result *runtime.Result) {
				completed <- result
			}})
			closed := make(chan struct{})
			id := pidapi.PID{UniqID: "completed"}
			proc, err := sched.Submit(context.Background(), id, &quickProcess{closed: closed}, "", nil)
			require.NoError(t, err)
			require.True(t, proc.queue.Push(process.Event{Type: process.EventMessage, Data: tc.pkg}, proc.gen.Load()))
			require.NotPanics(t, func() { sched.complete(proc, nil, nil) })
			select {
			case result := <-completed:
				require.NoError(t, result.Error)
				require.Equal(t, tc.outdated, result.Outdated)
			default:
				t.Fatal("completion does not acknowledge the terminal result")
			}
			select {
			case <-closed:
			default:
				t.Fatal("completion does not close the process")
			}
			require.Empty(t, sched.ListProcesses())
		})
	}
}
