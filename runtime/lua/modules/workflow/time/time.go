package time

import (
	stdtime "time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow"
	"github.com/wippyai/runtime/api/workflow/std"
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
	arg := l.Get(1)

	duration, err := origtime.ParseDurationValue(arg)
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	header := &std.TimerHeader{
		Milliseconds: duration.Milliseconds(),
	}
	req := upstream.NewRequest(l, std.TypeTimerSleep, nil, payload.New(header))
	return upstream.SendAndYield(l, req)
}

func after(l *lua.LState) int {
	arg := l.Get(1)

	// For raw numbers, treat as nanoseconds to match time constants (time.SECOND, etc.)
	// For strings and Duration objects, use ParseDurationValue
	var milliseconds int64
	if num, ok := arg.(lua.LNumber); ok {
		// Raw number - treat as nanoseconds
		milliseconds = int64(float64(num) / float64(stdtime.Millisecond))
	} else {
		// String or Duration object - use standard parsing
		duration, err := origtime.ParseDurationValue(arg)
		if err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		milliseconds = duration.Milliseconds()
	}

	header := &std.TimerHeader{
		Milliseconds: milliseconds,
	}
	req := upstream.NewRequest(l, std.TypeTimerSleep, nil, payload.New(header))
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
	arg := l.Get(1)

	// For raw numbers, treat as nanoseconds to match time constants (time.SECOND, etc.)
	// For strings and Duration objects, use ParseDurationValue
	var milliseconds int64
	if num, ok := arg.(lua.LNumber); ok {
		// Raw number - treat as nanoseconds
		milliseconds = int64(float64(num) / float64(stdtime.Millisecond))
	} else {
		// String or Duration object - use standard parsing
		duration, err := origtime.ParseDurationValue(arg)
		if err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		milliseconds = duration.Milliseconds()
	}

	header := &std.TimerHeader{
		Milliseconds: milliseconds,
	}
	req := upstream.NewRequest(l, std.TypeTimerSleep, nil, payload.New(header))
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
