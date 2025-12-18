package engine

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// TopicHandler processes incoming messages for a topic before channel delivery.
// Return value is what gets sent to the channel. Return nil to skip channel send.
type TopicHandler func(ctx context.Context, l *lua.LState, source pid.PID, topic string, payloads []payload.Payload) lua.LValue

// subscribeContext manages topic-to-channel mappings.
// The subscription owns the channel - channels are created here, not by callers.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	mu        sync.RWMutex
}

// add creates or returns an existing subscription for a topic.
// If the topic is already subscribed, returns the existing subscription.
// The bufSize parameter is only used when creating a new subscription.
// Subscription owns the channel - callers should not create channels.
func (m *subscribeContext) add(topic string, bufSize int) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		return existing, nil
	}

	ch := NewChannel(bufSize)
	sub := &subscription{topic: topic, channel: ch}
	m.byTopic[topic] = sub
	m.byChannel[ch] = topic
	return sub, nil
}

// addExisting registers an externally-owned channel for a topic.
// Used by modules that manage their own channel lifecycle (websocket, timer, etc.).
// Returns error if topic already has a different channel subscribed.
func (m *subscribeContext) addExisting(topic string, ch *Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, runtimelua.NewTopicAlreadySubscribedError(topic)
		}
		return existing, nil
	}

	sub := &subscription{topic: topic, channel: ch}
	m.byTopic[topic] = sub
	m.byChannel[ch] = topic
	return sub, nil
}

func (m *subscribeContext) remove(ch *Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	topic, exists := m.byChannel[ch]
	if !exists {
		return luaapi.ErrChannelNotFound
	}
	delete(m.byTopic, topic)
	delete(m.byChannel, ch)
	return nil
}

func (m *subscribeContext) get(topic string) (*subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, exists := m.byTopic[topic]
	return sub, exists
}

// match finds a subscription that matches the given topic.
// Supports glob-style patterns:
//   - "*" matches any topic
//   - "foo.*" matches "foo.bar", "foo.baz", etc.
//   - "foo.*.bar" matches "foo.x.bar", "foo.y.bar", etc.
func (m *subscribeContext) match(topic string) (*subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First try exact match
	if sub, exists := m.byTopic[topic]; exists {
		return sub, true
	}

	// Try pattern matching
	for pattern, sub := range m.byTopic {
		if matchTopicPattern(pattern, topic) {
			return sub, true
		}
	}
	return nil, false
}

// matchTopicPattern checks if topic matches the glob pattern.
// Pattern can contain * which matches any sequence of characters.
func matchTopicPattern(pattern, topic string) bool {
	// No wildcards - exact match only
	if pattern == topic {
		return true
	}

	// Simple glob matching
	pi, ti := 0, 0
	starIdx, matchIdx := -1, 0

	for ti < len(topic) {
		if pi < len(pattern) && pattern[pi] == '*' {
			starIdx = pi
			matchIdx = ti
			pi++
		} else if pi < len(pattern) && (pattern[pi] == topic[ti] || pattern[pi] == '?') {
			pi++
			ti++
		} else if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			ti = matchIdx
		} else {
			return false
		}
	}

	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// subscription links a topic to a channel.
type subscription struct {
	topic   string
	channel *Channel
}

// SubscribeRequest is yielded to request a topic subscription.
// If ExistingChannel is nil, subscription creates the channel.
// If ExistingChannel is set, it is used instead (for externally-owned channels).
type SubscribeRequest struct {
	Topic           string
	BufSize         int
	Handler         TopicHandler
	ExistingChannel *Channel
}

func (r *SubscribeRequest) String() string       { return "<subscribe_request>" }
func (r *SubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// UnsubscribeRequest is yielded to unsubscribe a channel.
type UnsubscribeRequest struct {
	Channel *Channel
}

func (r *UnsubscribeRequest) String() string       { return "<unsubscribe_request>" }
func (r *UnsubscribeRequest) Type() lua.LValueType { return lua.LTUserData }
