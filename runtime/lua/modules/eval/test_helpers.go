package eval

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

type mockEventBus struct{}

func (b *mockEventBus) Send(_ context.Context, _ event.Event) {}
func (b *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "mock", nil
}
func (b *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "mock", nil
}
func (b *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

type mockModule struct {
	name string
}

func (m *mockModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        m.name,
		Description: "mock module",
		Class:       []string{luaapi.ClassDeterministic},
	}
}

func (m *mockModule) Loader(l *lua.LState) int {
	t := l.CreateTable(0, 1)
	t.RawSetString("test", lua.LString("module_loaded"))
	l.Push(t)
	return 1
}
