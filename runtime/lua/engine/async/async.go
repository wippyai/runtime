package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

type scheduleCtx struct{}

type scheduleItem struct {
	ch    *channel.Channel
	value lua.LValue
	ok    bool
}

var scheduleKey = &scheduleCtx{}

// WithScheduleChannel attaches schedule channel to context
func WithScheduleChannel(ctx context.Context, ch chan scheduleItem) context.Context {
	return context.WithValue(ctx, scheduleKey, ch)
}

// GetScheduleChannel retrieves the schedule channel from context
func GetScheduleChannel(ctx context.Context) chan scheduleItem {
	if ch, ok := ctx.Value(scheduleKey).(chan scheduleItem); ok {
		return ch
	}
	return nil
}

// Send sends a value through the schedule channel and wakes up the task group
func Send(L *lua.LState, ch *channel.Channel, value lua.LValue, ok bool) {
	tg := engine.GetTaskGroup(L.Context())
	if tg == nil {
		return
	}

	sh := GetScheduleChannel(L.Context())
	if sh == nil {
		return
	}

	sh <- scheduleItem{ch: ch, value: value, ok: ok}
	tg.WakeUp()
}
