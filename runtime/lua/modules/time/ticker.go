package time

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/uow"
	lua "github.com/yuin/gopher-lua"
)

// Ticker represents a Lua userdata object wrapping time.Ticker
type Ticker struct {
	ticker  *time.Ticker
	chValue lua.LValue
}

// Constructor for ticker
func ticker(l *lua.LState) int {
	var duration time.Duration
	var err error

	// Get UoW from context
	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("time.ticker: unit of work missing")
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
			l.RaiseError("time.ticker: %s", err)
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

	// Register cleanup to stop ticker
	uw.AddCleanupFunc(tkr.Stop)

	// Spawn userdata for time value upfront
	timeUD := l.NewUserData()
	timeUD.Value = &Time{time: time.Now()} // initial value will be replaced
	l.SetMetatable(timeUD, l.GetTypeMetatable("Time"))

	// Launch goroutine to handle ticker
	go func() {
		for {
			select {
			case t := <-tkr.C:
				timeUD.Value = &Time{time: t}
				errs := async.Send(l, ch, timeUD, true)
				if errs != nil {
					return
				}
			case <-uw.Context().Done():
				return
			}
		}
	}()

	// Spawn and return Ticker userdata
	ud := l.NewUserData()
	ud.Value = &Ticker{ticker: tkr, chValue: channel.Wrap(l, ch)}
	l.SetMetatable(ud, l.GetTypeMetatable("Ticker"))
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
	t.ticker.Stop()
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

// Register Ticker
func registerTicker(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Ticker")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"stop":    tickerStop,
		"channel": tickerChannel,
	}))

	// Register ticker constructor
	l.SetField(mod, "ticker", l.NewFunction(ticker))
}
