package events

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	"github.com/wippyai/runtime/system/eventbus"
	lua "github.com/yuin/gopher-lua"
)

const (
	subscriptionTypeName = "events.Subscription"
)

var (
	moduleTable           *lua.LTable
	registration          *luaapi.Registration
	subscriptionMetatable *lua.LTable
	initOnce              sync.Once
)

// Module is the singleton events module instance.
var Module = &eventsModule{}

type eventsModule struct{}

func (m *eventsModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "events",
		Description: "Event bus subscribe and send",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *eventsModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()

		subscriptionMetatable = value.RegisterTypeMethods(nil, subscriptionTypeName,
			map[string]lua.LGFunction{"__tostring": subscriptionToString},
			subscriptionMethods)

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

type Subscription struct {
	subscriber    *eventbus.Subscriber
	channel       *engine.Channel
	system        event.System
	kind          event.Kind
	closed        bool
	mu            sync.Mutex
	cancelCleanup func()
}

func (m *eventsModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("subscribe", lua.LGoFunc(subscribe))
	mod.RawSetString("send", lua.LGoFunc(send))
	mod.Immutable = true
	return mod
}

var subscriptionMethods = map[string]lua.LGFunction{
	"channel": subscriptionChannel,
	"close":   subscriptionClose,
}

func subscribe(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("system pattern is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "events.subscribe", system, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to subscribe to events from system: %s", system)))
		return 2
	}

	var kind string
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		kind = l.CheckString(2)
	}

	bus := event.GetBus(ctx)
	if bus == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("event bus not found in context"))
		return 2
	}

	tc := payload.GetTranscoder(ctx)

	ch := engine.NewChannel(64)

	sub := &Subscription{
		channel: ch,
		system:  system,
		kind:    kind,
		closed:  false,
	}

	subscriber, err := eventbus.NewSubscriber(
		ctx,
		bus,
		system,
		kind,
		func(evt event.Event) {
			sub.mu.Lock()
			if sub.closed {
				sub.mu.Unlock()
				return
			}
			sub.mu.Unlock()

			evtTable := l.CreateTable(0, 4)
			evtTable.RawSetString("system", lua.LString(evt.System))
			evtTable.RawSetString("kind", lua.LString(evt.Kind))
			evtTable.RawSetString("path", lua.LString(evt.Path))

			if evt.Data != nil && tc != nil {
				luaPayload, err := tc.Transcode(payload.New(evt.Data), payload.Lua)
				if err == nil {
					if lv, ok := luaPayload.Data().(lua.LValue); ok {
						evtTable.RawSetString("data", lv)
					}
				}
			}

			ch.Send(nil, evtTable, nil)
		},
	)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create subscription: %v", err)))
		return 2
	}

	sub.subscriber = subscriber

	// Register cleanup with Store
	store := resource.GetStore(ctx)
	if store != nil {
		sub.cancelCleanup = store.AddCleanup(func() error {
			sub.mu.Lock()
			defer sub.mu.Unlock()
			if !sub.closed {
				sub.closed = true
				if sub.subscriber != nil {
					sub.subscriber.Close()
					sub.subscriber = nil
				}
				if sub.channel != nil {
					sub.channel.Close(nil)
				}
			}
			return nil
		})
	}

	value.NewUserData(l, sub, subscriptionMetatable)
	return 1
}

func send(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("no context"))
		return 2
	}

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

	if !security.IsAllowed(ctx, "events.send", system, nil) {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("not allowed to send events to system: %s", system)))
		return 2
	}

	bus := event.GetBus(ctx)
	if bus == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("event bus not found in context"))
		return 2
	}

	var data any
	if l.GetTop() >= 4 && l.Get(4) != lua.LNil {
		data = toGoValue(l.Get(4))
	}

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

func checkSubscription(l *lua.LState, idx int) *Subscription {
	ud := l.CheckUserData(idx)
	if sub, ok := ud.Value.(*Subscription); ok {
		return sub
	}
	l.ArgError(idx, "events.Subscription expected")
	return nil
}

func subscriptionChannel(l *lua.LState) int {
	sub := checkSubscription(l, 1)
	if sub == nil {
		return 0
	}

	ud := value.NewTypedUserData(l, sub.channel, "channel")
	if ud == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("channel type not registered"))
		return 2
	}
	return 1
}

func subscriptionClose(l *lua.LState) int {
	sub := checkSubscription(l, 1)
	if sub == nil {
		return 0
	}

	sub.mu.Lock()
	if sub.closed {
		sub.mu.Unlock()
		l.Push(lua.LTrue)
		return 1
	}

	sub.closed = true
	cancel := sub.cancelCleanup
	sub.cancelCleanup = nil

	if sub.subscriber != nil {
		sub.subscriber.Close()
		sub.subscriber = nil
	}

	if sub.channel != nil {
		sub.channel.Close(nil)
	}
	sub.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	l.Push(lua.LTrue)
	return 1
}

func subscriptionToString(l *lua.LState) int {
	sub := checkSubscription(l, 1)
	if sub == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("events.Subscription{system=%s, kind=%s}", sub.system, sub.kind)))
	return 1
}

func toGoValue(lv lua.LValue) any {
	switch v := lv.(type) {
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return nil
	case *lua.LTable:
		m := make(map[string]any)
		v.ForEach(func(k, val lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				m[string(ks)] = toGoValue(val)
			}
		})
		return m
	default:
		return nil
	}
}
