package engine

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// ProcessContextKey is the context key for ProcessContext in FrameContext.
// Set by Process.Execute before frame is sealed.
var ProcessContextKey = &ctxapi.Key{Name: "lua.process_context", Inherit: false}

// processContextPool reuses ProcessContext instances to reduce allocations.
var processContextPool = sync.Pool{
	New: func() any {
		return &ProcessContext{
			channels: make(map[*Channel]int, 4),
			subs: &subscribeContext{
				byTopic:   make(map[string]*subscription, 4),
				byChannel: make(map[*Channel]string, 4),
			},
		}
	},
}

// TopicHandler processes incoming messages for a topic before channel delivery.
// Return value is what gets sent to the channel. Return nil to skip channel send.
type TopicHandler func(ctx context.Context, l *lua.LState, payloads []payload.Payload) lua.LValue

// ProcessContext holds all request-specific state for a Lua process execution.
// Created once per Execute call, stored in FrameContext, released on completion.
// Note: Inbox is managed separately by the pool scheduler and accessed via FrameContext.
type ProcessContext struct {
	// Context reference for inbox lookup
	ctx context.Context

	// Channel layer state
	channelQueue *TaskQueue
	channels     map[*Channel]int

	// Subscribe layer state
	subs *subscribeContext

	// Topic handlers for custom message processing
	handlers   map[string]TopicHandler
	handlersMu sync.RWMutex

	// mu protects closed flag
	mu sync.Mutex
	// closed prevents double-release to pool
	closed bool
}

// acquireProcessContext gets a ProcessContext from the pool.
func acquireProcessContext() *ProcessContext {
	pc := processContextPool.Get().(*ProcessContext)
	pc.mu.Lock()
	pc.closed = false
	pc.mu.Unlock()
	return pc
}

// Close releases the ProcessContext back to the pool.
// Implements ctxapi.Closer for automatic cleanup when FrameContext is released.
// Safe to call multiple times - subsequent calls are no-ops.
func (pc *ProcessContext) Close() error {
	pc.mu.Lock()
	if pc.closed {
		pc.mu.Unlock()
		return nil
	}
	pc.closed = true
	pc.ctx = nil
	pc.mu.Unlock()

	pc.reset()
	processContextPool.Put(pc)
	return nil
}

// reset clears all ProcessContext fields for reuse.
func (pc *ProcessContext) reset() {
	// Clear channel layer
	if pc.channelQueue != nil {
		pc.channelQueue.Drain()
	}
	for k := range pc.channels {
		delete(pc.channels, k)
	}

	// Clear subscribe layer
	pc.subs.mu.Lock()
	for k := range pc.subs.byTopic {
		delete(pc.subs.byTopic, k)
	}
	for k := range pc.subs.byChannel {
		delete(pc.subs.byChannel, k)
	}
	pc.subs.mu.Unlock()

	// Clear handlers
	pc.handlersMu.Lock()
	for k := range pc.handlers {
		delete(pc.handlers, k)
	}
	pc.handlersMu.Unlock()
}

// ChannelQueue returns the channel layer task queue, creating it if needed.
func (pc *ProcessContext) ChannelQueue() *TaskQueue {
	if pc.channelQueue == nil {
		pc.channelQueue = NewTaskQueue()
	}
	return pc.channelQueue
}

// Channels returns the channel reference map.
func (pc *ProcessContext) Channels() map[*Channel]int {
	return pc.channels
}

// Subscriptions returns the subscription context.
func (pc *ProcessContext) Subscriptions() *subscribeContext {
	return pc.subs
}

// HasSubscriptions returns true if there are active subscriptions.
func (pc *ProcessContext) HasSubscriptions() bool {
	pc.subs.mu.RLock()
	defer pc.subs.mu.RUnlock()
	return len(pc.subs.byTopic) > 0
}

// Subscribe registers a channel to receive messages for a topic.
func (pc *ProcessContext) Subscribe(topic string, ch *Channel) error {
	_, err := pc.subs.add(topic, ch)
	return err
}

// Unsubscribe removes a channel's topic subscription.
func (pc *ProcessContext) Unsubscribe(ch *Channel) error {
	return pc.subs.remove(ch)
}

// SetTopicHandler registers a handler for a topic.
func (pc *ProcessContext) SetTopicHandler(topic string, handler TopicHandler) {
	pc.handlersMu.Lock()
	if pc.handlers == nil {
		pc.handlers = make(map[string]TopicHandler, 4)
	}
	pc.handlers[topic] = handler
	pc.handlersMu.Unlock()
}

// GetTopicHandler retrieves a handler for a topic.
func (pc *ProcessContext) GetTopicHandler(topic string) (TopicHandler, bool) {
	pc.handlersMu.RLock()
	h, ok := pc.handlers[topic]
	pc.handlersMu.RUnlock()
	return h, ok
}

// RemoveTopicHandler removes a handler for a topic.
func (pc *ProcessContext) RemoveTopicHandler(topic string) {
	pc.handlersMu.Lock()
	delete(pc.handlers, topic)
	pc.handlersMu.Unlock()
}

// GetProcessContext retrieves ProcessContext from FrameContext.
func GetProcessContext(ctx context.Context) *ProcessContext {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(ProcessContextKey); ok {
		return val.(*ProcessContext)
	}
	return nil
}

// setProcessContext stores ProcessContext in FrameContext.
// Note: Inbox is set separately by the pool scheduler before Init is called.
func setProcessContext(ctx context.Context, pc *ProcessContext) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	pc.ctx = ctx
	return fc.Set(ProcessContextKey, pc)
}
