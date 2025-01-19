package chromise

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"time"
)

type Module struct{}

func NewChromiseModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "chromise"
}

func (m *Module) Loader(L *lua.LState) int {
	mod := L.NewTable()
	L.SetField(mod, "time_after", L.NewFunction(timeAfter))
	L.Push(mod)
	return 1
}

func timeAfter(L *lua.LState) int {
	ms := L.CheckNumber(1)
	if ms <= 0 {
		L.RaiseError("time_after: duration must be > 0")
		return 0
	}

	ch := channel.Named(fmt.Sprintf("timer_%d", ms), 1)
	go func() {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-L.Context().Done():
			return
		}
		Send(L, ch, lua.LBool(true), true)
		Send(L, ch, lua.LNil, false) // close channel
	}()

	L.Push(channel.Wrap(L, ch))
	return 1
}
