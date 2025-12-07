package events

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	eventsapi "github.com/wippyai/runtime/api/dispatcher/events"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// EventSubscribeYield is yielded to subscribe to events from the bus.
type EventSubscribeYield struct {
	System  string
	Kind    string
	Channel *engine.Channel
	PID     relay.PID
	Topic   string
}

var eventSubscribeYieldPool = sync.Pool{
	New: func() interface{} { return &EventSubscribeYield{} },
}

func AcquireEventSubscribeYield(system, kind string, ch *engine.Channel, pid relay.PID, topic string) *EventSubscribeYield {
	y := eventSubscribeYieldPool.Get().(*EventSubscribeYield)
	y.System = system
	y.Kind = kind
	y.Channel = ch
	y.PID = pid
	y.Topic = topic
	return y
}

func ReleaseEventSubscribeYield(y *EventSubscribeYield) {
	y.System = ""
	y.Kind = ""
	y.Channel = nil
	y.PID = relay.PID{}
	y.Topic = ""
	eventSubscribeYieldPool.Put(y)
}

func (y *EventSubscribeYield) String() string       { return "<event_subscribe_yield>" }
func (y *EventSubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *EventSubscribeYield) CmdID() dispatcher.CommandID {
	return eventsapi.CmdEventsSubscribe
}

func (y *EventSubscribeYield) ToCommand() dispatcher.Command {
	return eventsapi.EventsSubscribeCmd{
		System: y.System,
		Kind:   y.Kind,
		Topic:  y.Topic,
		PID:    y.PID,
	}
}

func (y *EventSubscribeYield) Release() { ReleaseEventSubscribeYield(y) }

// HandleResult sets up the topic subscription and returns the channel.
func (y *EventSubscribeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	// Create channel userdata (PushChannel also sets ch.Value internally)
	ud := engine.PushChannel(l, y.Channel)
	l.Pop(1) // Remove from stack since we return via slice

	// Try to subscribe channel to topic if we're in a process context
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.Subscribe(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, lua.LString(err.Error())}
		}
		proc.SetTopicHandler(y.Topic, eventMessageHandler)
	}

	// Register cleanup to unsubscribe from dispatcher when frame is released
	if sub, ok := data.(eventsapi.EventSubscription); ok && sub.Unsubscribe != nil {
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					sub.Unsubscribe()
					return nil
				})
			}
		}
	}

	return []lua.LValue{ud, lua.LNil}
}

// eventMessageHandler converts event payloads to Lua tables.
func eventMessageHandler(ctx context.Context, l *lua.LState, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	p := payloads[0]
	data, ok := p.Data().(map[string]any)
	if !ok {
		return lua.LNil
	}

	tbl := lua.CreateTable(0, 4)
	if v, ok := data["system"].(string); ok {
		tbl.RawSetString("system", lua.LString(v))
	}
	if v, ok := data["kind"].(string); ok {
		tbl.RawSetString("kind", lua.LString(v))
	}
	if v, ok := data["path"].(string); ok {
		tbl.RawSetString("path", lua.LString(v))
	}
	if v, ok := data["data"]; ok {
		tbl.RawSetString("data", anyToLua(l, v))
	}

	return tbl
}

// anyToLua converts Go values to Lua values.
func anyToLua(l *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case string:
		return lua.LString(val)
	case bool:
		return lua.LBool(val)
	case float64:
		return lua.LNumber(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case map[string]any:
		tbl := lua.CreateTable(0, len(val))
		for k, v := range val {
			tbl.RawSetString(k, anyToLua(l, v))
		}
		return tbl
	case []any:
		tbl := lua.CreateTable(len(val), 0)
		for i, v := range val {
			tbl.RawSetInt(i+1, anyToLua(l, v))
		}
		return tbl
	default:
		return lua.LNil
	}
}

// EventSendYield is yielded to send an event to the bus.
type EventSendYield struct {
	System string
	Kind   string
	Path   string
	Data   any
}

var eventSendYieldPool = sync.Pool{
	New: func() interface{} { return &EventSendYield{} },
}

func AcquireEventSendYield(system, kind, path string, data any) *EventSendYield {
	y := eventSendYieldPool.Get().(*EventSendYield)
	y.System = system
	y.Kind = kind
	y.Path = path
	y.Data = data
	return y
}

func ReleaseEventSendYield(y *EventSendYield) {
	y.System = ""
	y.Kind = ""
	y.Path = ""
	y.Data = nil
	eventSendYieldPool.Put(y)
}

func (y *EventSendYield) String() string       { return "<event_send_yield>" }
func (y *EventSendYield) Type() lua.LValueType { return lua.LTUserData }

func (y *EventSendYield) CmdID() dispatcher.CommandID {
	return eventsapi.CmdEventsSend
}

func (y *EventSendYield) ToCommand() dispatcher.Command {
	return eventsapi.EventsSendCmd{
		System: y.System,
		Kind:   y.Kind,
		Path:   y.Path,
		Data:   y.Data,
	}
}

func (y *EventSendYield) Release() { ReleaseEventSendYield(y) }

// HandleResult returns success after the event is sent.
func (y *EventSendYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
