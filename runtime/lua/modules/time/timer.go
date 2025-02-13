package time

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

// Timer represents a Lua userdata object wrapping time.Timer
type Timer struct {
	timer   *time.Timer
	chValue lua.LValue
}

// Constructor for timer
func timer(l *lua.LState) int {
	var duration time.Duration
	var err error

	switch v := l.Get(1).(type) {
	case *lua.LUserData:
		if d, ok := v.Value.(*Duration); ok {
			duration = d.duration
		} else {
			l.ArgError(1, "duration expected")
			return 0
		}
	case lua.LString:
		duration, err = time.ParseDuration(string(v))
		if err != nil {
			l.RaiseError("time.timer: %s", err)
			return 0
		}
	case lua.LNumber:
		duration = time.Duration(float64(v) * float64(time.Millisecond))
	default:
		l.ArgError(1, "duration, string, or number expected")
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.timer: duration must be > 0")
		return 0
	}

	// Spawn channel and timer
	ch := channel.Named(fmt.Sprintf("timer_%s", duration), 1)
	tmr := time.NewTimer(duration)

	// Spawn userdata for time value upfront
	timeUD := l.NewUserData()
	timeUD.Value = &Time{time: time.Now()} // initial value will be replaced
	l.SetMetatable(timeUD, l.GetTypeMetatable("Time"))

	// Launch goroutine to handle timer
	go func() {
		defer tmr.Stop()
		select {
		case t := <-tmr.C:
			timeUD.Value = &Time{time: t}
			errs := async.Send(l, ch, timeUD, true)
			if errs != nil {
				l.RaiseError("time.timer: %s", errs)
				return
			}
		case <-l.Context().Done():
			return
		}
	}()

	// Spawn and return Timer userdata
	ud := l.NewUserData()
	ud.Value = &Timer{timer: tmr, chValue: channel.Wrap(l, ch)}
	l.SetMetatable(ud, l.GetTypeMetatable("Timer"))
	l.Push(ud)
	return 1
}

func isTimer(l *lua.LState, n int) (*Timer, bool) {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if t, ok := ud.Value.(*Timer); ok {
			return t, true
		}
	}
	return nil, false
}

// Timer methods implementations
func timerStop(l *lua.LState) int {
	t, ok := isTimer(l, 1)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}

	stopped := t.timer.Stop()
	l.Push(lua.LBool(stopped))
	return 1
}

func timerReset(l *lua.LState) int {
	t, ok := isTimer(l, 1)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}

	var duration time.Duration
	var err error

	switch v := l.Get(2).(type) {
	case *lua.LUserData:
		if d, ok := v.Value.(*Duration); ok {
			duration = d.duration
		} else {
			l.ArgError(2, "duration expected")
			return 0
		}
	case lua.LString:
		duration, err = time.ParseDuration(string(v))
		if err != nil {
			l.RaiseError("timer:reset: %s", err)
			return 0
		}
	case lua.LNumber:
		duration = time.Duration(float64(v) * float64(time.Millisecond))
	default:
		l.ArgError(2, "duration, string, or number expected")
		return 0
	}

	if duration <= 0 {
		l.RaiseError("timer:reset: duration must be > 0")
		return 0
	}

	l.Push(lua.LBool(t.timer.Reset(duration)))
	return 1
}

func timerChannel(l *lua.LState) int {
	t, ok := isTimer(l, 1)
	if !ok {
		l.ArgError(1, "timer expected")
		return 0
	}
	l.Push(t.chValue)
	return 1
}

// Register Timer
func registerTimer(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Timer")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"stop":    timerStop,
		"reset":   timerReset,
		"channel": timerChannel,
	}))

	// Register timer constructor
	l.SetField(mod, "timer", l.NewFunction(timer))
}
