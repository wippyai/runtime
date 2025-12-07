// Package events provides event bus dispatcher for routing events to Lua processes.
package events

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	eventsapi "github.com/wippyai/runtime/api/dispatcher/events"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// subscription tracks a single process subscription.
type subscription struct {
	pid    relay.PID
	system string
	kind   string
	topic  string
}

// Dispatcher routes event bus events to subscribed Lua processes.
// It maintains a single subscription to the event bus and routes
// events internally based on pattern matching.
type Dispatcher struct {
	bus    event.Bus
	node   relay.Node
	subID  event.SubscriberID
	eventC chan event.Event

	mu   sync.RWMutex
	subs map[string]*subscription // topic -> subscription

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewDispatcher creates a new events dispatcher.
func NewDispatcher(bus event.Bus, node relay.Node) *Dispatcher {
	return &Dispatcher{
		bus:    bus,
		node:   node,
		subs:   make(map[string]*subscription),
		eventC: make(chan event.Event, 64),
	}
}

// Start subscribes to the event bus and starts the routing loop.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)

	// Subscribe to all events (system "*")
	var err error
	d.subID, err = d.bus.Subscribe(d.ctx, "*", d.eventC)
	if err != nil {
		return err
	}

	d.wg.Add(1)
	go d.routeLoop()

	return nil
}

// Stop unsubscribes and stops the routing loop.
func (d *Dispatcher) Stop(ctx context.Context) error {
	d.cancel()
	d.bus.Unsubscribe(ctx, d.subID)
	close(d.eventC)
	d.wg.Wait()
	return nil
}

// routeLoop receives events and routes them to subscribed processes.
func (d *Dispatcher) routeLoop() {
	defer d.wg.Done()

	for evt := range d.eventC {
		d.routeEvent(evt)
	}
}

// routeEvent sends an event to all matching subscriptions.
func (d *Dispatcher) routeEvent(evt event.Event) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, sub := range d.subs {
		if !matchPattern(sub.system, string(evt.System)) {
			continue
		}
		if sub.kind != "" && sub.kind != "*" && !matchPattern(sub.kind, string(evt.Kind)) {
			continue
		}

		// Build event payload as map
		data := map[string]any{
			"system": string(evt.System),
			"kind":   string(evt.Kind),
			"path":   evt.Path,
		}
		if evt.Data != nil {
			data["data"] = evt.Data
		}

		pkg := relay.NewPackage(relay.PID{}, sub.pid, relay.Topic(sub.topic), payload.New(data))
		_ = d.node.Send(pkg)
	}
}

// matchPattern checks if value matches a glob pattern.
// Supports * as wildcard for any sequence.
func matchPattern(pattern, value string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}
	// Simple prefix match for patterns like "system.*"
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}
	return pattern == value
}

// RegisterAll registers event bus command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(eventsapi.CmdEventsSubscribe, dispatcher.HandlerFunc(d.handleSubscribe))
	register(eventsapi.CmdEventsSend, dispatcher.HandlerFunc(d.handleSend))
}

func (d *Dispatcher) handleSubscribe(ctx context.Context, cmd dispatcher.Command, tag any, receiver process.ResultReceiver) error {
	subCmd := cmd.(eventsapi.EventsSubscribeCmd)

	d.mu.Lock()
	d.subs[subCmd.Topic] = &subscription{
		pid:    subCmd.PID,
		system: subCmd.System,
		kind:   subCmd.Kind,
		topic:  subCmd.Topic,
	}
	d.mu.Unlock()

	receiver.CompleteYield(tag, eventsapi.EventSubscription{
		System: subCmd.System,
		Kind:   subCmd.Kind,
		Topic:  subCmd.Topic,
	}, nil)

	return nil
}

func (d *Dispatcher) handleSend(ctx context.Context, cmd dispatcher.Command, tag any, receiver process.ResultReceiver) error {
	sendCmd := cmd.(eventsapi.EventsSendCmd)

	evt := event.Event{
		System: event.System(sendCmd.System),
		Kind:   event.Kind(sendCmd.Kind),
		Path:   sendCmd.Path,
		Data:   sendCmd.Data,
	}

	d.bus.Send(ctx, evt)
	receiver.CompleteYield(tag, true, nil)

	return nil
}

// Unsubscribe removes a subscription by topic.
func (d *Dispatcher) Unsubscribe(topic string) {
	d.mu.Lock()
	delete(d.subs, topic)
	d.mu.Unlock()
}
