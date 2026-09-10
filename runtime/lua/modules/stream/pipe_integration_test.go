// SPDX-License-Identifier: MPL-2.0
package stream_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
	"github.com/wippyai/runtime/runtime/lua/engine"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/runtime/system/scheduler"
	actor "github.com/wippyai/runtime/system/scheduler/actor"
	streamsys "github.com/wippyai/runtime/system/stream"
)

type pipeFixture struct {
	calls  atomic.Int32
	closed atomic.Bool
}
type fixtureReader struct {
	io.Reader
	owner *pipeFixture
}

func (r *fixtureReader) Close() error { r.owner.closed.Store(true); return nil }
func (f *pipeFixture) Allocate(ctx, lifetime context.Context, peer string, limit uint64) (io.ReadCloser, string, error) {
	f.calls.Add(1)
	p, ok := runtimeapi.GetFramePID(ctx)
	if !ok || p.UniqID != "pipe-agent" || peer != "approved-peer" || limit != 64 || ctx.Err() != nil || lifetime.Err() != nil {
		return nil, "", errors.New("fixture admission denied")
	}
	return &fixtureReader{Reader: strings.NewReader("pipe bytes"), owner: f}, "opaque-offer", nil
}

type pipeCompletion chan *runtimeapi.Result

func (pipeCompletion) OnStart(context.Context, pid.PID, process.Process) error         { return nil }
func (c pipeCompletion) OnComplete(_ context.Context, _ pid.PID, r *runtimeapi.Result) { c <- r }

func TestLuaAllocatesCanonicalPipeThroughScheduler(t *testing.T) {
	for _, mode := range []string{"allowed", "missing_allocator", "wrong_peer", "invalid_limit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
			defer frame.Close()
			caller := pid.PID{Node: "local", Host: "lua", UniqID: "pipe-agent"}
			if err := runtimeapi.SetFramePID(ctx, caller); err != nil {
				t.Fatal(err)
			}
			store := resource.NewStore()
			defer store.Close()
			if err := resource.SetStore(ctx, store); err != nil {
				t.Fatal(err)
			}
			fixture := &pipeFixture{}
			if mode != "missing_allocator" {
				if err := streamapi.SetPipeAllocator(ctx, fixture); err != nil {
					t.Fatal(err)
				}
			}
			dispatcher := streamsys.NewDispatcher()
			if err := dispatcher.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer dispatcher.Stop(context.Background())
			registry := scheduler.NewRegistry()
			dispatcher.RegisterAll(registry.Register)
			registry.Freeze()
			done := make(pipeCompletion, 1)
			sched := actor.NewScheduler(registry, actor.WithWorkers(2), actor.WithLifecycle(done))
			sched.Start()
			defer func() {
				stop, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				sched.Stop(stop)
			}()
			script := `local s,offer,e=stream.pipe("approved-peer",64); assert(s and offer=="opaque-offer" and not e); local text,e=s:read(64); assert(text=="pipe bytes" and not e); local text,e=s:read(64); assert(text==nil and e==nil); assert(s:close()); return "done"`
			if mode != "allowed" {
				peer := "approved-peer"
				limit := "64"
				if mode == "wrong_peer" {
					peer = "other"
				}
				if mode == "invalid_limit" {
					limit = "-1"
				}
				script = `local s,o,e=stream.pipe("` + peer + `",` + limit + `); assert(s==nil and o==nil and e~=nil); return "denied"`
			}
			proc, err := engine.NewProcess(engine.WithScript(script, "pipe.lua"), engine.WithModuleBinder(func(l *lua.LState) error { engine.LoadModuleDef(l, streammod.Module); return nil }))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sched.Submit(ctx, caller, proc, "", nil); err != nil {
				proc.Close()
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.Error != nil {
					t.Fatal(result.Error)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Lua pipe timed out")
			}
			expected := int32(1)
			if mode == "missing_allocator" || mode == "invalid_limit" {
				expected = 0
			}
			if fixture.calls.Load() != expected {
				t.Fatalf("allocator calls=%d", fixture.calls.Load())
			}
			if mode == "allowed" && !fixture.closed.Load() {
				t.Fatal("stream close did not close endpoint")
			}
		})
	}
}
