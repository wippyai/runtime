package lua

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Execute executes a Lua function with the given arguments
func (m *RuntimeManager) Execute(task runtime.Task) (chan *runtime.Result, error) {
	// Get the callable from the sync map
	cl, ok := m.callable.Load(task.Target)
	if !ok {
		return nil, fmt.Errorf("handler not found for target: %s", task.Target)
	}

	handler, ok := cl.(api.Callable)
	if !ok {
		return nil, fmt.Errorf("handler is not a callable")
	}

	// Get the function configuration
	fn, exists := m.functions.Get(task.Target)
	if !exists {
		return nil, fmt.Errorf("function configuration not found for target: %s", task.Target)
	}

	// Convert payloads to Lua values
	args := make([]lua.LValue, 0, len(task.Payloads))
	if len(task.Payloads) > 0 {
		dtt, ok := task.Context.Value(contextapi.TranscoderCtx).(payload.Transcoder)
		if !ok {
			return nil, fmt.Errorf("transcoder not found in context")
		}

		for _, p := range task.Payloads {
			local, err := dtt.Transcode(p, payload.Lua)
			if err != nil {
				return nil, fmt.Errorf("failed to transcode payload: %w", err)
			}

			args = append(args, local.Data().(lua.LValue))
		}
	}

	// Create execution context with task ID
	ctx, cancel := context.WithCancel(
		context.WithValue(task.Context, contextapi.TaskCtx, task.Target),
	)
	defer cancel()

	// Execute the function
	result, err := handler.Execute(ctx, fn.Method, args...)

	// Create result channel with buffer size 1
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(result, payload.Lua),
		Error:   err,
	}
	close(resultChan)

	return resultChan, nil
}
