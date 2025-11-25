package subscribe

import (
	"fmt"
	"sync/atomic"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

var chanID = atomic.Uint64{}

type subscribe struct {
	topic   string
	channel *channel.Channel
}

func (r *subscribe) String() string {
	return "subscribe.subscribe{topic=" + r.topic + "}"
}

func (r *subscribe) Type() lua.LValueType {
	return lua.LTUserData
}

type unsubscribe struct {
	channel *channel.Channel
}

func (r *unsubscribe) String() string {
	return "subscribe.unsubscribe{}"
}

func (r *unsubscribe) Type() lua.LValueType {
	return lua.LTUserData
}

// Module represents pubsub Lua module
type Module struct{}

// NewSubscribeModule creates a new pubsub module instance
func NewSubscribeModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "subscribe",
		Description: "Pub/sub topic subscription",
		Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	// Spawn module table
	mod := l.NewTable()

	// Register functions
	l.SetField(mod, "subscribe", l.NewFunction(subscribeFunc))
	l.SetField(mod, "unsubscribe", l.NewFunction(unsubscribeFunc))

	// Register module
	l.Push(mod)
	return 1
}

func subscribeFunc(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.RaiseError("topic cannot be empty")
		return 0
	}

	chName := fmt.Sprintf("subscribe.%s.%d", topic, chanID.Add(1))
	ch := channel.Named(chName, 1)
	return Subscribe(l, ch, topic)
}

func unsubscribeFunc(l *lua.LState) int {
	ch := channel.CheckChannel(l)
	return Unsubscribe(l, ch)
}

// Subscribe subscribes to a topic and returns a channel
func Subscribe(l *lua.LState, ch *channel.Channel, topic string) int {
	req := &subscribe{
		topic:   topic,
		channel: ch,
	}
	l.Push(req)
	return -1
}

// Unsubscribe unsubscribes from a topic via channel
func Unsubscribe(l *lua.LState, ch *channel.Channel) int {
	req := &unsubscribe{channel: ch}
	l.Push(req)
	return -1
}
