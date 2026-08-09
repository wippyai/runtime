// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

type execCompletion struct {
	data any
	err  error
}

type execResultReceiver struct {
	once sync.Once
	done chan execCompletion
}

func (r *execResultReceiver) CompleteYield(_ uint64, data any, err error) {
	r.once.Do(func() {
		r.done <- execCompletion{data: data, err: err}
	})
}

// waitForExecResult dispatches process.exec and does not return until the
// launched process has produced an ExecResult or the caller is canceled. The
// command is intentionally not pooled here: a cancellation may race with a
// handler's final callback, and retaining this small command until the
// callback is harmless while avoiding use-after-release hazards.
func waitForExecResult(ctx context.Context, command *process.ExecCmd) (*runtime.Result, error) {
	d := dispatcher.GetDispatcher(ctx)
	if d == nil {
		return nil, fmt.Errorf("process dispatcher not available")
	}
	handler := d.Dispatch(command)
	if handler == nil {
		return nil, fmt.Errorf("process exec handler not available")
	}

	receiver := &execResultReceiver{done: make(chan execCompletion, 1)}
	if err := handler.Handle(ctx, command, 1, receiver); err != nil {
		return nil, err
	}

	select {
	case completion := <-receiver.done:
		if completion.err != nil {
			return nil, completion.err
		}
		result, ok := completion.data.(process.ExecResult)
		if !ok {
			return nil, fmt.Errorf("process exec returned %T, want process.ExecResult", completion.data)
		}
		return result.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
