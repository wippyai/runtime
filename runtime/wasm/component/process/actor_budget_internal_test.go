package process

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/wasm-runtime/memory/budget"
)

func TestActorFactoryAggregateMemoryAdmission(t *testing.T) {
	code, err := os.ReadFile("../../engine/testdata/internal-validation/two-core.wasm")
	require.NoError(t, err)
	for _, pages := range []int64{4, 5} {
		cfg := &api.ProcessConfig{Method: "func1"}
		cfg.SetOptions(api.ProcessOptions{Limits: api.ProcessLimitsConfig{MemoryBytes: pages * 65536}})
		factory := NewActorFactory(code, true, cfg, wasmcomponent.NewHostRegistry(), nil)
		defer factory.Close()
		var live []*wasmengine.ActorProcess
		for index := 0; index < 2; index++ {
			proc, err := factory.Create()()
			require.NoError(t, err)
			actor := proc.(*wasmengine.ActorProcess)
			defer actor.Close()
			ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
			defer frame.Close()
			require.NoError(t, runtimeapi.SetFramePID(ctx, pid.PID{Node: "local", Host: "budget-test", UniqID: string(rune('a' + index))}))
			require.NotNil(t, actor.MemoryBudget())
			require.Equal(t, uint64(pages*65536), actor.MemoryBudget().Usage().Limit)
			require.NoError(t, actor.Init(ctx, "func1", []payload.Payload{payload.New("resident")}))
			var out processapi.StepOutput
			err = actor.Step(nil, &out)
			if pages == 4 {
				// Each individual core fits (2 and 3 pages), but their aggregate does not.
				require.ErrorIs(t, err, budget.ErrLimit)
				require.Zero(t, actor.MemoryBudget().Usage().Used)
				continue
			}
			require.NoError(t, err)
			require.True(t, out.IsDone())
			require.Equal(t, uint64(5*65536), actor.MemoryBudget().Usage().Peak)
			require.Zero(t, actor.MemoryBudget().Usage().Used, "completed actor must release memory")
			live = append(live, actor)
		}
		if len(live) == 2 {
			require.NotSame(t, live[0].MemoryBudget(), live[1].MemoryBudget())
			live[0].Close()
			require.Zero(t, live[0].MemoryBudget().Usage().Used)
			require.Equal(t, uint64(5*65536), live[1].MemoryBudget().Usage().Peak)
			live[1].Close()
			require.Zero(t, live[1].MemoryBudget().Usage().Used)
		}
	}
}
