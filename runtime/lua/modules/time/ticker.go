package time

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"

	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

// Ticker represents a Lua userdata object wrapping time.Ticker
type Ticker struct {
	ticker  *time.Ticker
	chValue lua.LValue
	done    chan struct{}
	release context.CancelFunc
}

// Constructor for ticker
func ticker(l *lua.LState) int {
	var duration time.Duration
	var err error

	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("time.Ticker: unit of work missing")
		return 0
	}

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
			l.RaiseError("time.Ticker: %s", err)
			return 0
		}
	case lua.LNumber:
		duration = time.Duration(float64(v) * float64(time.Millisecond))
	default:
		l.ArgError(1, "duration, string, or number expected")
		return 0
	}

	if duration <= 0 {
		l.RaiseError("time.ticker: duration must be > 0")
		return 0
	}

	// Spawn channel and ticker
	ch := channel.Named(fmt.Sprintf("ticker_%s", duration), 1)
	tkr := time.NewTicker(duration)
	done := make(chan struct{})

	// Create ticker object
	tickerObj := &Ticker{
		ticker:  tkr,
		chValue: channel.Wrap(l, ch),
		done:    done,
	}

	// With the proposed change to resourceManager.AddCleanup,
	// calling release will both execute the cleanup function AND remove it from the list
	tickerObj.release = uw.AddCleanup(func() error {
		select {
		case <-done:
			// Already stopped
		default:
			tkr.Stop()
			close(done)
		}
		return nil
	})

	timeUD := l.NewUserData()
	timeUD.Value = &Time{time: time.Now()}
	timeUD.Metatable = value.GetTypeMetatable(l, "time.Time")

	uw.Run(func(uw engine.UnitOfWork) {
		for {
			select {
			case t := <-tkr.C:
				timeUD.Value = &Time{time: t}
				errs := channel.Send(l, ch, timeUD)
				if errs != nil {
					return
				}
			case <-done:
				// Explicitly stopped
				return
			case <-uw.Context().Done():
				// UoW completed
				return
			}
		}
	})

	// Create and return Ticker userdata
	ud := l.NewUserData()
	ud.Value = tickerObj
	ud.Metatable = value.GetTypeMetatable(l, "time.Ticker")
	l.Push(ud)
	return 1
}

func isTicker(l *lua.LState, n int) (*Ticker, bool) {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if t, ok := ud.Value.(*Ticker); ok {
			return t, true
		}
	}
	return nil, false
}

// Ticker methods implementations
func tickerStop(l *lua.LState) int {
	t, ok := isTicker(l, 1)
	if !ok {
		l.ArgError(1, "ticker expected")
		return 0
	}

	select {
	case <-t.done:
		// Already stopped
	default:
		// Just call release - it will both stop the ticker AND remove from UoW cleanup
		if t.release != nil {
			t.release()
			t.release = nil
		}
	}

	return 0
}

func tickerChannel(l *lua.LState) int {
	t, ok := isTicker(l, 1)
	if !ok {
		l.ArgError(1, "ticker expected")
		return 0
	}
	l.Push(t.chValue)
	return 1
}
