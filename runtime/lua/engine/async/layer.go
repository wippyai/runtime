package async

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

type scheduleCtx struct{}

type async struct {
	from  *lua.LState
	ch    *channel.Channel
	value lua.LValue
	ok    bool
}

var scheduleKey = &scheduleCtx{}

// getContext retrieves the async channel from context
func getContext(ctx context.Context) (*engine.TaskGroup, chan async, error) {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return nil, nil, errors.New("cannot send from non-task context")
	}

	if ch, ok := ctx.Value(scheduleKey).(chan async); ok {
		return tg, ch, nil
	}

	return nil, nil, errors.New("no async channel found in context")
}

// Send sends a value through the async channel and wakes up the task group
func Send(L *lua.LState, ch *channel.Channel, value lua.LValue, ok bool) error {
	tg, asyncCh, err := getContext(L.Context())
	if err != nil {
		return err
	}

	select {
	case asyncCh <- async{from: L, ch: ch, value: value, ok: ok}:
		tg.WakeUp()
	case <-L.Context().Done():
		return errors.New("context has been cancelled")
	}
	return nil
}

// Layer processes scheduled operations
type Layer struct {
	channels *channel.Layer
	schedule chan async
}

func NewAsyncLayer(channels *channel.Layer, chanSize int) *Layer {
	return &Layer{
		channels: channels,
		schedule: make(chan async, chanSize),
	}
}

func (r *Layer) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, scheduleKey, r.schedule)
}

// Step implements the engine.Layer interface
func (r *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	select {
	case item := <-r.schedule:
		if item.ok {
			err := r.channels.Send(item.from.Context(), item.ch, item.value)
			if err != nil {
				return outTasks, nil // Log error but continue
			}
		} else {
			err := r.channels.Close(item.from.Context(), item.ch)
			if err != nil {
				return outTasks, nil // Log error but continue
			}
		}
	default:
		// No items to process
	}

	return outTasks, nil
}
