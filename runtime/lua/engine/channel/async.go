package channel

import (
	"errors"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Send schedules value send to channel.
func Send(l *lua.LState, ch *Channel, value ...lua.LValue) error {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		return errors.New("no unit of work found")
	}

	return uw.Tasks().Schedule(func() {
		err := send(l.Context(), ch, value...)
		if err != nil {
			// log.Printf("error sending value to channel: %v", err)
		}
	})
}

// Close schedules channel close.
func Close(l *lua.LState, ch *Channel) error {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		return errors.New("no unit of work found")
	}

	return uw.Tasks().Schedule(func() {
		err := closeChannel(l.Context(), ch)
		if err != nil {
			// log.Printf("error closing channel: %v", err)
		}
	})
}
