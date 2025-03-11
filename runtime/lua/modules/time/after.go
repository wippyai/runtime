package time

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

func after(l *lua.LState) int {
	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("time.After: unit of work missing")
		return 0
	}

	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("time.After: %s", err)
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.After: duration must be > 0")
		return 0
	}

	// Create channel using UoW context
	ch := channel.Named(fmt.Sprintf("timer_%s", duration), 1)

	uw.Run(func(uw engine.UnitOfWork) {
		select {
		case <-time.After(duration):
			if err := channel.Send(l, ch, lua.LBool(true)); err != nil {
				return
			}
		case <-uw.Context().Done():
			return
		}

		_ = channel.Close(l, ch)
	})

	l.Push(channel.Wrap(l, ch))
	return 1
}
