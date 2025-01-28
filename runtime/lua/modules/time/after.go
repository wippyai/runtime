package time

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

func after(l *lua.LState) int {
	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("time.after: %s", err)
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.after: duration must be > 0")
		return 0
	}

	ch := channel.Named(fmt.Sprintf("timer_%s", duration), 1)
	go func() {
		select {
		case <-time.After(duration):
		case <-l.Context().Done():
			return
		}
		_ = async.Send(l, ch, lua.LBool(true), true)
		_ = async.Send(l, ch, lua.LNil, false) // close channel
	}()

	l.Push(channel.Wrap(l, ch))
	return 1
}
