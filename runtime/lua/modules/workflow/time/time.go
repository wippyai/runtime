package time

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	origtime "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
)

func now(l *lua.LState) int {
	ref := workflow.GetTimeReference(l.Context())
	if ref == nil {
		l.RaiseError("TimeReference not found in context")
		return 0
	}

	t := ref.Now()
	ud := l.NewUserData()
	ud.Value = origtime.NewTime(t)
	ud.Metatable = value.GetTypeMetatable(l, "time.Time")

	l.Push(ud)
	return 1
}

func sleep(l *lua.LState) int {
	duration, err := origtime.ParseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	req := upstream.NewRequest(l, "timer.sleep", nil, payload.New(duration.Milliseconds()))
	return upstream.SendAndYield(l, req)
}

func after(l *lua.LState) int {
	duration, err := origtime.ParseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	req := upstream.NewRequest(l, "timer.sleep", nil, payload.New(duration.Milliseconds()))
	timerUD := l.NewUserData()
	timerUD.Value = &Timer{req: req}
	timerUD.Metatable = value.GetTypeMetatable(l, "time.Timer")

	up, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.RaiseError("no upstream handler found in context")
		return 0
	}
	if err := up.SendRequest(req); err != nil {
		l.RaiseError("failed to send timer request: %s", err.Error())
		return 0
	}

	l.Push(timerUD)
	return 1
}

func timer(l *lua.LState) int {
	duration, err := origtime.ParseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	req := upstream.NewRequest(l, "timer.sleep", nil, payload.New(duration.Milliseconds()))
	timerUD := l.NewUserData()
	timerUD.Value = &Timer{req: req, resettable: true}
	timerUD.Metatable = value.GetTypeMetatable(l, "time.Timer")

	l.Push(timerUD)
	return 1
}

func ticker(l *lua.LState) int {
	l.RaiseError("time.ticker() is not supported in workflows - tickers require multiple responses which conflicts with workflow determinism")
	return 0
}
