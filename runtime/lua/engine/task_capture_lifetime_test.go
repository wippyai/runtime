// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestRuntimeTaskTeardownPreservesCaptures(t *testing.T) {
	for _, path := range []string{"task close", "main completion cleanup"} {
		t.Run(path, func(t *testing.T) {
			state := lua.NewState()
			defer state.Close()
			proc := &Process{state: state, ctx: context.Background(), queue: NewTaskQueue()}
			fn, err := state.LoadString(`
local value = 42
coroutine.yield(function() return value end)
`)
			require.NoError(t, err)
			task := proc.createTask(fn)
			require.Same(t, task, proc.queue.Pop())
			status, values, err := state.Resume(task.Thread(), task.Function())
			require.NoError(t, err)
			require.Equal(t, lua.ResumeYield, status)
			require.Len(t, values, 1)
			read := values[0].(*lua.LFunction)
			if path == "task close" {
				proc.removeTask(task)
			} else {
				// vmStep uses this cleanup when the main task completes,
				// while the process's main Lua state remains allocated.
				proc.killAllThreads()
			}
			require.Empty(t, proc.threads)
			require.True(t, proc.queue.IsEmpty())
			require.NoError(t, state.CallByParam(lua.P{Fn: read, NRet: 1, Protect: true}))
			require.Equal(t, lua.LNumber(42), state.ToNumber(-1))
			state.Pop(1)
		})
	}
}
