package processmod

import (
	"fmt"
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
)

// Channel context keys for UoW storage
var (
	inboxChannel  = &context.Key{Name: "process.channel.inbox"}
	eventsChannel = &context.Key{Name: "process.channel.events"}
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

	// Add our methods to the process table
	processTable.RawSetString("get_options", l.NewFunction(m.getOptions))
	processTable.RawSetString("set_options", l.NewFunction(m.setOptions))

	// No need to return anything as we're modifying the global module
	return 0
}

// getOptions retrieves current process options from UoW
// Returns: options table
func (m *Module) getOptions(l *lua.LState) int {
	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Create options table
	options := l.CreateTable(0, 1) // Initial size for known options

	// Get existing options from UoW or use defaults
	procVal, found := uw.Values().Get(process.StateKey)
	if !found {
		l.RaiseError("invalid operational context")
		return 0
	}

	proc, ok := procVal.(*process.State)
	if !ok {
		l.RaiseError("invalid operational context")
		return 0
	}

	options.RawSetString("trap_links", lua.LBool(proc.IsTrapLinksEnabled()))

	l.Push(options)
	return 1

}

// setOptions configures process options
// Params: options_table
// Returns: success, error
func (m *Module) setOptions(l *lua.LState) int {
	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Validate that argument is a table
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTTable {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("options parameter must be a table"))
		return 2
	}

	// Get process state from UoW
	procVal, found := uw.Values().Get(process.StateKey)
	if !found {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("invalid operational context"))
		return 2
	}

	proc, ok := procVal.(*process.State)
	if !ok {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("invalid operational context"))
		return 2
	}

	options := l.CheckTable(1)

	// Track if we found any unsupported options
	unsupportedOption := ""

	// Process trap_links option if present
	if trapLinks := options.RawGetString("trap_links"); trapLinks != lua.LNil {
		if trapLinks.Type() != lua.LTBool {
			l.Push(lua.LBool(false))
			l.Push(lua.LString("trap_links must be a boolean"))
			return 2
		}

		proc.SetTrapLinks(lua.LVAsBool(trapLinks))

		m.log.Debug("trap_links setting changed",
			zap.Bool("enable", lua.LVAsBool(trapLinks)))
	}

	// Check for any other options - first one becomes error
	options.ForEach(func(k lua.LValue, v lua.LValue) {
		if k.Type() == lua.LTString {
			keyStr := string(k.(lua.LString))
			if keyStr != "trap_links" && unsupportedOption == "" {
				unsupportedOption = keyStr
			}
		}
	})

	// If we found an unsupported option, return error
	if unsupportedOption != "" {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(fmt.Sprintf("option %s is not supported", unsupportedOption)))
		return 2
	}

	// Return success
	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
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
