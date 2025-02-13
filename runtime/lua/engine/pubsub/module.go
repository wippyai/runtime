package pubsub

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync/atomic"
)

var chanID = atomic.Uint64{}

type subscribe struct {
	topic   string
	channel *channel.Channel
}

func (r *subscribe) String() string {
	return "pubsub.subscribe{topic=" + r.topic + "}"
}

func (r *subscribe) Type() lua.LValueType {
	return lua.LTUserData
}

type unsubscribe struct {
	channel *channel.Channel
}

func (r *unsubscribe) String() string {
	return "pubsub.unsubscribe{}"
}

func (r *unsubscribe) Type() lua.LValueType {
	return lua.LTUserData
}

// Module represents pubsub Lua module
type Module struct{}

// NewModule creates a new pubsub module instance
func NewModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "pubsub"
}

// Loader registers the module functions
func (m *Module) Loader(L *lua.LState) int {
	// Spawn module table
	mod := L.NewTable()

	// Register functions
	L.SetField(mod, "subscribe", L.NewFunction(subscribeFunc))
	L.SetField(mod, "unsubscribe", L.NewFunction(unsubscribeFunc))

	// Register module
	L.Push(mod)
	return 1
}

func subscribeFunc(L *lua.LState) int {
	topic := L.CheckString(1)
	if topic == "" {
		L.RaiseError("topic cannot be empty")
		return 0
	}

	chName := fmt.Sprintf("pubsub.%s.%d", topic, chanID.Add(1))
	ch := channel.Named(chName, 1)
	return Subscribe(L, ch, topic)
}

func unsubscribeFunc(L *lua.LState) int {
	ch := channel.CheckChannel(L)
	return Unsubscribe(L, ch)
}

// Subscribe subscribes to a topic and returns a channel
func Subscribe(L *lua.LState, ch *channel.Channel, topic string) int {
	req := &subscribe{
		topic:   topic,
		channel: ch,
	}
	L.Push(req)
	return -1
}

// Unsubscribe unsubscribes from a topic via channel
func Unsubscribe(L *lua.LState, ch *channel.Channel) int {
	req := &unsubscribe{channel: ch}
	L.Push(req)
	return -1
}
