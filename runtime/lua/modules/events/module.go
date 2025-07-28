package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/security"
	"github.com/ponyruntime/pony/system/eventbus"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	subscriptionMetatable = "events.Subscription"
)

// Module represents the events module for Lua
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
}

// NewEventsModule creates a new events module
func NewEventsModule(log *zap.Logger) *Module {
	return &Module{
		log: log.Named("events"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "events"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Create module table with pre-allocated size
	mod := l.CreateTable(0, 2) // 2 functions: subscribe and send

	// Register top-level subscribe function (without needing to get the bus first)
	mod.RawSetString("subscribe", l.NewFunction(func(l *lua.LState) int {
		return m.subscribe(l)
	}))

	// Register top-level send function
	mod.RawSetString("send", l.NewFunction(func(l *lua.LState) int {
		return m.send(l)
	}))

	// Make the table immutable so it can be safely reused
	mod.Immutable = true

	// Register subscription metatable
	value.RegisterMethods(l, subscriptionMetatable, map[string]lua.LGFunction{
		"channel": m.subChannel,
		"close":   m.subClose,
	})

	m.moduleTable = mod
}

// LuaSubscription wraps an eventbus Subscriber for Lua
type LuaSubscription struct {
	subscriber *eventbus.Subscriber
	ch         *channel.Channel
	system     event.System
	kind       event.Kind
	log        *zap.Logger
	closed     bool
	release    context.CancelFunc // Cancel function from UoW's AddCleanup
}

// subscribe creates a new subscription to the event bus
func (m *Module) subscribe(l *lua.LState) int {
	// Get system and optional kind pattern
	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("system pattern is required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "events.subscribe", system, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to subscribe to events from system: %s", system)))
		return 2
	}

	// Optional kind pattern
	var kind string
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		kind = l.CheckString(2)
	}

	// Get context and UoW
	ctx := l.Context()
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("unit of work not found in context"))
		return 2
	}

	// Get event bus from context
	bus := event.GetBus(ctx)
	if bus == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("event bus not found in context"))
		return 2
	}

	// Important: Use the UoW context to ensure proper lifecycle management
	uwCtx := uw.Context()

	// Create a channel for events
	ch := channel.Named(fmt.Sprintf("event_%s_%s", system, kind), 64)

	// Create LuaSubscription object
	luaSub := &LuaSubscription{
		ch:     ch,
		system: system,
		kind:   kind,
		log:    m.log,
		closed: false,
	}

	// Create the actual subscriber with a handler that sends to our channel
	subscriber, err := eventbus.NewSubscriber(
		uwCtx, // Use UoW context to ensure proper lifecycle
		bus,
		system,
		kind,
		func(evt event.Event) {
			// Convert event to Lua table
			evtTable := l.CreateTable(0, 4)
			evtTable.RawSetString("system", lua.LString(evt.System))
			evtTable.RawSetString("kind", lua.LString(evt.Kind))
			evtTable.RawSetString("path", lua.LString(evt.Path))

			// Convert event data to Lua
			if evt.Data != nil {
				luaData, err := luaconv.GoToLua(evt.Data)
				if err != nil {
					m.log.Error("failed to convert event data to Lua",
						zap.Error(err),
						zap.String("system", evt.System),
						zap.String("kind", evt.Kind))
				} else {
					evtTable.RawSetString("data", luaData)
				}
			}

			// Check if context is still valid
			select {
			case <-uwCtx.Done():
				return
			default:
				// Send the event to our channel
				if err := channel.Send(l, ch, evtTable); err != nil {
					m.log.Error("failed to send event to channel",
						zap.Error(err),
						zap.String("system", evt.System),
						zap.String("kind", evt.Kind))
				}
			}
		},
	)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create subscription: %v", err)))
		return 2
	}

	// Store the subscriber
	luaSub.subscriber = subscriber

	// Register cleanup function to ensure subscriber is closed when UoW is done
	luaSub.release = uw.AddCleanup(func() error {
		if !luaSub.closed && luaSub.subscriber != nil {
			luaSub.subscriber.Close()
			luaSub.subscriber = nil
			luaSub.closed = true
		}
		return channel.Close(l, ch)
	})

	// Create userdata
	ud := l.NewUserData()
	ud.Value = luaSub
	ud.Metatable = value.GetTypeMetatable(l, subscriptionMetatable)

	// Return subscription object
	l.Push(ud)
	return 1
}

// send publishes an event to the event bus
func (m *Module) send(l *lua.LState) int {
	// Get required parameters
	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("system is required"))
		return 2
	}

	kind := l.CheckString(2)
	if kind == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("kind is required"))
		return 2
	}

	path := l.CheckString(3)
	if path == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("path is required"))
		return 2
	}

	// Check security permissions
	if !security.IsAllowed(l.Context(), "events.send", system, nil) {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("not allowed to send events to system: %s", system)))
		return 2
	}

	// Get context
	ctx := l.Context()

	// Get event bus from context
	bus := event.GetBus(ctx)
	if bus == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("event bus not found in context"))
		return 2
	}

	// Optional data parameter
	var data any
	if l.GetTop() >= 4 && l.Get(4) != lua.LNil {
		data = luaconv.ToGoAny(l.Get(4))
	}

	// Create and send event
	evt := event.Event{
		System: system,
		Kind:   kind,
		Path:   path,
		Data:   data,
	}

	bus.Send(ctx, evt)

	l.Push(lua.LTrue)
	return 1
}

// subChannel returns the channel for the subscription
func (m *Module) subChannel(l *lua.LState) int {
	// Get subscription
	sub := checkSubscription(l)
	if sub == nil {
		return 0
	}

	// Return the channel
	l.Push(channel.Wrap(l, sub.ch))
	return 1
}

// subClose closes the subscription
func (m *Module) subClose(l *lua.LState) int {
	// Get subscription
	sub := checkSubscription(l)
	if sub == nil {
		return 0
	}

	if sub.closed {
		l.Push(lua.LTrue) // Already closed
		return 1
	}

	// Mark as closed
	sub.closed = true

	// Close the underlying subscriber
	if sub.subscriber != nil {
		sub.subscriber.Close()
		sub.subscriber = nil
	}

	// IMPORTANT: Call the cancel function to remove cleanup from UoW
	if sub.release != nil {
		sub.release()
		sub.release = nil
	}

	// Close the channel
	if err := channel.Close(l, sub.ch); err != nil {
		m.log.Error("failed to close subscription channel",
			zap.Error(err),
			zap.String("system", sub.system),
			zap.String("kind", sub.kind))
	}

	l.Push(lua.LTrue)
	return 1
}

// checkSubscription validates and returns the subscription from userdata
func checkSubscription(l *lua.LState) *LuaSubscription {
	ud := l.CheckUserData(1)
	if sub, ok := ud.Value.(*LuaSubscription); ok {
		return sub
	}
	l.ArgError(1, "event subscription expected")
	return nil
}
