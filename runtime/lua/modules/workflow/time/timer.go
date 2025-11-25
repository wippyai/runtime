package time

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	origtime "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
)

type Timer struct {
	req        *upstream.Request
	resettable bool
}

func timerStop(l *lua.LState) int {
	ud := l.CheckUserData(1)
	timer, ok := ud.Value.(*Timer)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}

	_ = timer.req.Cancel()

	l.Push(lua.LBool(true))
	return 1
}

func timerReset(l *lua.LState) int {
	ud := l.CheckUserData(1)
	timer, ok := ud.Value.(*Timer)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}

	duration, err := origtime.ParseDurationValue(l.Get(2))
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}

	if !timer.resettable {
		l.RaiseError("timer is not resettable")
		return 0
	}

	_ = timer.req.Cancel()

	header := &std.TimerHeader{
		Milliseconds: duration.Milliseconds(),
	}
	newReq := upstream.NewRequest(l, std.TypeTimerSleep, nil, payload.New(header))
	timer.req = newReq

	upstream, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.RaiseError("no upstream handler found in context")
		return 0
	}
	if err := upstream.SendRequest(newReq); err != nil {
		l.RaiseError("failed to send timer request: %s", err.Error())
		return 0
	}

	l.Push(lua.LBool(true))
	return 1
}

func timerChannel(l *lua.LState) int {
	ud := l.CheckUserData(1)
	timer, ok := ud.Value.(*Timer)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}

	return channel.Receive(l, timer.req.GetChannel())
}
