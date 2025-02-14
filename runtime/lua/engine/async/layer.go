// Package async provides asynchronous operation handling for the Lua runtime engine,
// allowing non-blocking communication between Lua states and channels.
package async

import (
	"context"
	"errors"
	capi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

type async struct {
	from  *lua.LState
	ch    *channel.Channel
	value lua.LValue
	ok    bool
}

// getContext retrieves the async channel from context along with the task group.
// Returns an error if either the task group is not present or the async channel
// is not found in the context.
func getContext(ctx context.Context) (*engine.TaskGroup, chan async, error) {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return nil, nil, errors.New("cannot send from non-task context")
	}

	if ch, ok := ctx.Value(capi.AsyncCtx).(chan async); ok {
		return tg, ch, nil
	}

	return nil, nil, errors.New("no async channel found in context")
}

// Send sends a value through the async channel and wakes up the task group.
// This function is used to schedule asynchronous operations from Lua state.
// The 'ok' parameter determines if this is a send (true) or close (false) operation.
func Send(l *lua.LState, ch *channel.Channel, value lua.LValue, ok bool) error {
	tg, asyncCh, err := getContext(l.Context())
	if err != nil {
		return err
	}

	select {
	case asyncCh <- async{from: l, ch: ch, value: value, ok: ok}:
		tg.WakeUp()
	case <-l.Context().Done():
		return errors.New("context has been canceled")
	}
	return nil
}

func Close(l *lua.LState, ch *channel.Channel) error {
	return Send(l, ch, lua.LNil, false)
}

// Layer processes scheduled asynchronous operations by managing communication
// between Lua states and channels. It implements the engine.Layer interface
// for integration with the VM execution pipeline.
type Layer struct {
	channels *channel.Layer
	schedule chan async
}

// NewAsyncLayer creates a new async processing layer with the specified channel
// layer and buffer size for the scheduling channel. The channel layer is used
// for actual channel operations, while the async layer manages scheduling.
func NewAsyncLayer(channels *channel.Layer, chanSize int) *Layer {
	return &Layer{
		channels: channels,
		schedule: make(chan async, chanSize),
	}
}

// WithContext creates a new context containing the async scheduling channel.
// This allows the Send function to access the scheduling channel through context.
func (r *Layer) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, capi.AsyncCtx, r.schedule)
}

// Step implements the engine.Layer interface by processing scheduled async
// operations after executing VM tasks. It handles both send and close operations
// on channels, continuing execution even if individual operations fail.
func (r *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	select {
	case item := <-r.schedule:
		ctx := item.from.Context()
		if ctx.Err() != nil {
			return outTasks, nil
		}

		if item.ok {
			err := r.channels.Send(ctx, item.ch, item.value)
			if err != nil {
				return outTasks, nil // Log error but continue
			}
		} else {
			err := r.channels.Close(ctx, item.ch)
			if err != nil {
				return outTasks, nil // Log error but continue
			}
		}
	default:
		// No items to process
	}

	return outTasks, nil
}
