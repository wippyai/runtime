package time

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"time"
)

func after(l *lua.LState) int {
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
			l.RaiseError("time.after: %s", err)
			return 0
		}
	case lua.LNumber:
		duration = time.Duration(float64(v) * float64(time.Millisecond))
	default:
		l.ArgError(1, "duration, string, or number expected")
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.after: duration must be > 0")
		return 0
	}

	if err := async.ValidateContext(l); err != nil {
		l.RaiseError("time.after: %s", err)
		return 0
	}

	ch := channel.Named(fmt.Sprintf("timer_%s", duration), 1)
	go func() {
		select {
		case <-time.After(duration):
		case <-l.Context().Done():
			return
		}
		async.Send(l, ch, lua.LBool(true), true)
		async.Send(l, ch, lua.LNil, false) // close channel
	}()

	l.Push(channel.Wrap(l, ch))
	return 1
}
