package async

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

type scheduleCtx struct{}

type schedule struct {
	ch    *channel.Channel
	value lua.LValue
	ok    bool
}

var scheduleKey = &scheduleCtx{}

// WithAsyncChannel attaches schedule channel to context
func WithAsyncChannel(ctx context.Context) context.Context {
	return context.WithValue(ctx, scheduleKey, make(chan schedule, 4096))
}

// getAsyncChannel retrieves the schedule channel from context
func getAsyncChannel(ctx context.Context) chan schedule {
	if ch, ok := ctx.Value(scheduleKey).(chan schedule); ok {
		return ch
	}
	return nil
}

func ValidateContext(L *lua.LState) error {
	tg := engine.GetTaskGroup(L.Context())
	if tg == nil {
		return errors.New("cannot send from non-task context")
	}

	sh := getAsyncChannel(L.Context())
	if sh == nil {
		return errors.New("cannot send from non-task context")
	}

	return nil
}

// Send sends a value through the schedule channel and wakes up the task group
func Send(L *lua.LState, ch *channel.Channel, value lua.LValue, ok bool) {
	tg := engine.GetTaskGroup(L.Context())
	if tg == nil {

		return
	}

	sh := getAsyncChannel(L.Context())
	if sh == nil {
		return
	}

	select {
	case sh <- schedule{ch: ch, value: value, ok: ok}:
	default:
		// Channel is full
	}
	tg.WakeUp()
}
