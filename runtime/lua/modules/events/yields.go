// SPDX-License-Identifier: MPL-2.0

package events

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const subscriptionTypeName = "events.Subscription"

type Subscription struct {
	channelUD     *lua.LUserData
	channel       *engine.Channel
	unsubscribe   func()
	cancelCleanup func() // cancel handle from resource.Store.AddCleanup; must run on close.
	topic         string
	proc          *engine.Process
	closed        bool
	mu            sync.Mutex
}

func init() {
	value.RegisterTypeMethods(nil, subscriptionTypeName,
		map[string]lua.LGoFunc{"__tostring": subscriptionToString},
		map[string]lua.LGoFunc{
			"channel": subscriptionChannel,
			"close":   subscriptionClose,
		})
}

func checkSubscription(l *lua.LState) *Subscription {
	ud := l.CheckUserData(1)
	if sub, ok := ud.Value.(*Subscription); ok {
		return sub
	}
	l.ArgError(1, "subscription expected")
	return nil
}

func subscriptionToString(l *lua.LState) int {
	l.Push(lua.LString("events.Subscription{}"))
	return 1
}

func subscriptionChannel(l *lua.LState) int {
	sub := checkSubscription(l)
	if sub == nil {
		return 0
	}
	l.Push(sub.channelUD)
	return 1
}

func subscriptionClose(l *lua.LState) int {
	sub := checkSubscription(l)
	if sub == nil {
		return 0
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if sub.closed {
		l.Push(lua.LTrue)
		return 1
	}

	sub.closed = true
	if sub.unsubscribe != nil {
		sub.unsubscribe()
		sub.unsubscribe = nil
	}
	// Release the frame-store cleanup slot so a long-running process
	// that opens many subscriptions doesn't accumulate dead cleanup
	// closures.
	if sub.cancelCleanup != nil {
		sub.cancelCleanup()
		sub.cancelCleanup = nil
	}
	// Detach the channel from the process subscription map and close it.
	// engine.deliverMessage uses subs.byChannel / subs.byTopic; without
	// this both maps grow with every events.subscribe call.
	if sub.proc != nil && sub.channel != nil {
		sub.proc.UnsubscribeChannel(sub.channel)
	} else if sub.channel != nil {
		sub.channel.Close(nil)
	}

	l.Push(lua.LTrue)
	return 1
}

// EventSubscribeYield is yielded to subscribe to events from the bus.
type EventSubscribeYield struct {
	System  string
	Kind    string
	Channel *engine.Channel
	PID     pid.PID
	Topic   string
}

var eventSubscribeYieldPool = sync.Pool{
	New: func() any { return &EventSubscribeYield{} },
}

func AcquireEventSubscribeYield(system, kind string, ch *engine.Channel, p pid.PID, topic string) *EventSubscribeYield {
	y := eventSubscribeYieldPool.Get().(*EventSubscribeYield)
	y.System = system
	y.Kind = kind
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseEventSubscribeYield(y *EventSubscribeYield) {
	y.System = ""
	y.Kind = ""
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	eventSubscribeYieldPool.Put(y)
}

func (y *EventSubscribeYield) String() string       { return "<event_subscribe_yield>" }
func (y *EventSubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *EventSubscribeYield) CmdID() dispatcher.CommandID {
	return event.Subscribe
}

func (y *EventSubscribeYield) ToCommand() dispatcher.Command {
	return event.SubscribeCmd{
		System: y.System,
		Kind:   y.Kind,
		Topic:  y.Topic,
		PID:    y.PID,
	}
}

func (y *EventSubscribeYield) Release() { ReleaseEventSubscribeYield(y) }

// HandleResult sets up the topic subscription and returns a Subscription object.
func (y *EventSubscribeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "subscribe")}
	}

	// Create channel userdata (PushChannel also sets ch.Value internally)
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1) // Remove from stack since we return via slice

	// Try to subscribe externally-owned channel to topic if we're in a process context
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "subscribe")}
		}
	}

	// Create subscription with channel and unsubscribe function
	sub := &Subscription{
		channelUD: channelUD,
		channel:   y.Channel,
		topic:     y.Topic,
		proc:      proc,
	}

	// Store unsubscribe function from dispatcher
	if eventSub, ok := data.(event.Subscription); ok && eventSub.Unsubscribe != nil {
		sub.unsubscribe = eventSub.Unsubscribe

		// Register cleanup to unsubscribe from dispatcher when frame is
		// released, and save the cancel handle so sub:close() can release
		// the slot — otherwise long-running processes that repeatedly
		// subscribe accumulate dead closures in the frame store.
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				sub.cancelCleanup = store.AddCleanup(func() error {
					sub.mu.Lock()
					defer sub.mu.Unlock()
					if !sub.closed && sub.unsubscribe != nil {
						sub.unsubscribe()
						sub.unsubscribe = nil
					}
					return nil
				})
			}
		}
	}

	// Wrap in Subscription userdata
	subUD := value.PushTypedUserData(l, sub, subscriptionTypeName)
	l.Pop(1) // Remove from stack since we return via slice

	return []lua.LValue{subUD, lua.LNil}
}

// EventSendYield is yielded to send an event to the bus.
type EventSendYield struct {
	Data   any
	System string
	Kind   string
	Path   string
}

var eventSendYieldPool = sync.Pool{
	New: func() any { return &EventSendYield{} },
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
	return event.Send
}

func (y *EventSendYield) ToCommand() dispatcher.Command {
	return event.SendCmd{
		System: y.System,
		Kind:   y.Kind,
		Path:   y.Path,
		Data:   y.Data,
	}
}

func (y *EventSendYield) Release() { ReleaseEventSendYield(y) }

// HandleResult returns success after the event is sent.
func (y *EventSendYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "send event")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
