package processmod

import (
	"fmt"
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
)

// Channel context keys for UoW storage
var (
	inboxChannel  = &context.Key{Name: "process.pubsub.inbox"}
	eventsChannel = &context.Key{Name: "process.pubsub.events"}
)

// Module provides pubsub-based inbox and channel functionality for long-running processes
type Module struct {
	log *zap.Logger
}

// NewProcessAPIModule creates a new pubsub-based inbox module
func NewProcessAPIModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "process_api"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Get the existing process module from global scope
	v := l.GetGlobal("process")
	if v.Type() != lua.LTTable {
		m.log.Error("process table not found")
		return 0
	}

	// Get process table
	processTable := v.(*lua.LTable)

	// Register process-specific methods
	processTable.RawSetString("inbox", l.NewFunction(m.inbox))
	processTable.RawSetString("events", l.NewFunction(m.events))
	processTable.RawSetString("listen", l.NewFunction(m.listen))

	// No need to return anything as we're modifying the global module
	return 0
}

// inbox creates a special inbox channel for receiving messages that don't match other topics
func (m *Module) inbox(l *lua.LState) int {
	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Check if channel already exists in UoW
	existingChannel, found := uw.Values().Get(inboxChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	ch := channel.Named(topology.TopicInbox, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicInbox)

	// If subscription was successful, store the channel in UoW
	if result == 1 {
		// The channel is returned on the Lua stack, we need to get it
		channelWrapper := l.Get(-1)
		uw.Values().Set(inboxChannel, channelWrapper)
	}

	return result
}

// events creates a channel for system events
func (m *Module) events(l *lua.LState) int {
	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Check if channel already exists in UoW
	existingChannel, found := uw.Values().Get(eventsChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	// Create new channel for events
	ch := channel.Named(topology.TopicEvents, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicEvents)

	// If subscription was successful, store the channel in UoW
	if result == 1 {
		// The channel is returned on the Lua stack, we need to get it
		channelWrapper := l.Get(-1)
		uw.Values().Set(eventsChannel, channelWrapper)
	}

	return result
}

// listen creates a channel for specific topic listening
// This is NOT cached as these are dynamic subscriptions that may be unsubscribed later
func (m *Module) listen(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("topic cannot be empty"))
		return 2
	}

	// Prevent usage of @ topics in ports
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot use @ topics"))
		return 2
	}

	// Create new channel for the topic - NOT cached
	portName := fmt.Sprintf("listen.%s", topic)
	ch := channel.Named(portName, 1)

	// Return the subscription result directly
	return subscribe.Subscribe(l, ch, topic)
}
