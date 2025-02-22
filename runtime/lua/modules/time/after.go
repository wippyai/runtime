package time

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/uow"
	lua "github.com/yuin/gopher-lua"
)

func after(l *lua.LState) int {
	// Get UoW from context
	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("time.after: unit of work missing")
		return 0
	}

	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		l.RaiseError("time.after: %s", err)
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.after: duration must be > 0")
		return 0
	}

	// Create channel using UoW context
	ch := channel.Named(fmt.Sprintf("timer_%s", duration), 1)

	// Start timer goroutine
	go func() {
		select {
		case <-time.After(duration):
			// Timer completed, send result
			if err := async.Send(l, ch, lua.LBool(true), true); err != nil {
				// Log error if needed
				return
			}
		case <-uw.Context().Done():
			// UoW closed, cleanup
			return
		}

		// Close channel after sending or when cancelled
		_ = async.Send(l, ch, lua.LNil, false)
	}()

	l.Push(channel.Wrap(l, ch))
	return 1
}
