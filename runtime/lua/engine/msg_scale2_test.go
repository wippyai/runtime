package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	lua "github.com/wippyai/go-lua"
)

func TestMessageScalingReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping message scaling test in short mode")
	}
	script := `
		local ch = channel.new(1000)
		subscribe("msg", ch)
		while true do
			ch:receive()
		end
	`
	proto, _ := lua.CompileString(script, "test.lua")

	testCases := []int{1, 2, 4, 8, 16, 32}

	for _, numProcs := range testCases {
		t.Run(fmt.Sprintf("%d_procs", numProcs), func(t *testing.T) {
			procs := make([]*Process, numProcs)

			for i := 0; i < numProcs; i++ {
				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				proc := mustNewProcess(t, WithProto(proto))
				proc.Init(ctx, "", nil)
				LoadModuleDef(proc.State(), ChannelModule)
				loadPubSubGlobals(proc.State())

				var out process.StepOutput
				for j := 0; j < 20; j++ {
					out.Reset()
					_ = proc.Step(nil, &out)
					if out.Status() == process.StepIdle {
						break
					}
				}
				procs[i] = proc
			}

			var ops atomic.Int64
			var wg sync.WaitGroup
			stop := make(chan struct{})

			testPayload := payload.NewPayload(lua.LString("x"), payload.Lua)

			for i := 0; i < numProcs; i++ {
				wg.Add(1)
				go func(proc *Process) {
					defer wg.Done()
					var out process.StepOutput
					events := make([]process.Event, 1)
					for {
						select {
						case <-stop:
							return
						default:
							// Create new package each iteration since Step() releases it back to pool
							pkg := relay.NewPackage(pid.PID{}, pid.PID{}, "msg", testPayload)
							events[0] = process.Event{Type: process.EventMessage, Data: pkg}
							out.Reset()
							_ = proc.Step(events, &out)
							ops.Add(1)
						}
					}
				}(procs[i])
			}

			time.Sleep(2 * time.Second)
			close(stop)
			wg.Wait()

			for _, p := range procs {
				p.Close()
			}

			rate := float64(ops.Load()) / 2.0 / 1e6
			perProc := rate / float64(numProcs)
			t.Logf("%d procs: %.2f M/sec total, %.3f M/sec per-proc", numProcs, rate, perProc)
		})

		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
}
