package eventbus

import (
	"fmt"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/system/eventbus"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// LuaSubscription wraps a Subscriber for Lua
type LuaSubscription struct {
	subscriber *eventbus.Subscriber
	ch         *channel.Channel
	system     event.System
	kind       event.Kind
	log        *zap.Logger
}

// subscribe creates a new subscription to the event bus
func (m *Module) subscribe(l *lua.LState, bus event.Bus) int {
	// Get system and optional kind pattern
	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("system pattern is required"))
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
	}

	// Create the actual subscriber with a handler that sends to our channel
	subscriber, err := eventbus.NewSubscriber(
		uwCtx, // Use UoW context to ensure proper lifecycle
		bus,
		system,
		kind,
		func(evt event.Event) {
			// Convert event to Lua table
			evtTable := l.NewTable()
			evtTable.RawSetString("system", lua.LString(evt.System))
			evtTable.RawSetString("kind", lua.LString(evt.Kind))
			evtTable.RawSetString("path", lua.LString(evt.Path))

			// Convert event data to Lua
			if evt.Data != nil {
				luaData, err := luaconv.GoToLua(evt.Data)
				if err != nil {
					m.log.Error("failed to convert event data to Lua",
						zap.Error(err),
						zap.String("system", string(evt.System)),
						zap.String("kind", string(evt.Kind)))
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
						zap.String("system", string(evt.System)),
						zap.String("kind", string(evt.Kind)))
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
	uw.AddCleanup(func() error {
		if luaSub.subscriber != nil {
			luaSub.subscriber.Close()
			luaSub.subscriber = nil
		}
		return channel.Close(l, ch)
	})

	// Create userdata
	ud := l.NewUserData()
	ud.Value = luaSub
	l.SetMetatable(ud, l.GetTypeMetatable(subscriptionMetatable))

	// Return subscription object
	l.Push(ud)
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

	// Close the underlying subscriber
	if sub.subscriber != nil {
		sub.subscriber.Close()
		sub.subscriber = nil
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
