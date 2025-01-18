package timer

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"time"
)

// Lua module implementation
type Module struct{}

func NewTimerModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "timer"
}

// Loader implements the module loader for Lua
func (m *Module) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetFuncs(mod, map[string]lua.LGFunction{
		"sleep": sleepFunc,
		"after": afterFunc,
	})

	L.Push(mod)
	return 1
}

// sleepFunc implements the timer.sleep() function in Lua
func sleepFunc(L *lua.LState) int {
	duration := time.Duration(L.CheckNumber(1) * float64(time.Second))

	ctx := L.Context()
	tc := ctx.Value(timerContextKey).(*TimerContext)

	// Generate unique channel name for this timer
	channelName := generateUniqueChannelName("sleep")

	// Start the timer
	tc.startTimer(channelName, duration)

	// Return named channel for the caller to wait on
	ch := channel.Named(channelName, 1)
	ud := L.NewUserData()
	ud.Value = ch
	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)

	return 1
}

// afterFunc implements the timer.after() function in Lua
func afterFunc(L *lua.LState) int {
	duration := time.Duration(L.CheckNumber(1) * float64(time.Second))

	ctx := L.Context()
	tc := ctx.Value(timerContextKey).(*TimerContext)

	// Generate unique channel name for this timer
	channelName := generateUniqueChannelName("after")

	// Start the timer
	tc.startTimer(channelName, duration)

	// Return named channel for the caller to wait on
	ch := channel.Named(channelName, 1)
	ud := L.NewUserData()
	ud.Value = ch
	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)

	return 1
}
