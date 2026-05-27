// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

// TopicHandler processes incoming messages for a topic before channel delivery.
// Return value is what gets sent to the channel. Return nil to skip channel send.
type TopicHandler func(ctx context.Context, l *lua.LState, source pid.PID, topic string, payloads []payload.Payload) lua.LValue

// subscribeContext manages topic-to-channel mappings.
// The subscription owns the channel - channels are created here, not by callers.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	nextID    uint64
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
	sub := m.newSubscriptionLocked(topic, ch)
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

	sub := m.newSubscriptionLocked(topic, ch)
	m.byTopic[topic] = sub
	m.byChannel[ch] = topic
	return sub, nil
}

func (m *subscribeContext) newSubscriptionLocked(topic string, ch *Channel) *subscription {
	m.nextID++
	return &subscription{topic: topic, channel: ch, id: m.nextID}
}

func (m *subscribeContext) remove(ch *Channel) error {
	_, err := m.removeAndReturnTopic(ch)
	return err
}

// removeAndReturnTopic removes a channel's subscription and returns the topic
// that was registered for it. Callers use the returned topic to clean up the
// matching handler entry via Process.RemoveTopicHandler.
func (m *subscribeContext) removeAndReturnTopic(ch *Channel) (string, error) {
	topic, _, err := m.removeAndReturnSubscription(ch)
	return topic, err
}

func (m *subscribeContext) removeAndReturnSubscription(ch *Channel) (string, *subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	topic, exists := m.byChannel[ch]
	if !exists {
		return "", nil, luaapi.ErrChannelNotFound
	}
	sub := m.byTopic[topic]
	delete(m.byTopic, topic)
	delete(m.byChannel, ch)
	return topic, sub, nil
}

func (m *subscribeContext) get(topic string) (*subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, exists := m.byTopic[topic]
	return sub, exists
}

// match finds a subscription that matches the given topic.
// Only exact match - no glob patterns.
func (m *subscribeContext) match(topic string) (*subscription, bool) {
	m.mu.RLock()
	sub, exists := m.byTopic[topic]
	m.mu.RUnlock()
	return sub, exists
}

func (m *subscribeContext) snapshotAndClear() []*subscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.byTopic) == 0 {
		return nil
	}
	out := make([]*subscription, 0, len(m.byTopic))
	for _, sub := range m.byTopic {
		out = append(out, sub)
	}
	clear(m.byTopic)
	clear(m.byChannel)
	return out
}

// snapshotSubscriptions returns the live subscriptions without clearing the
// maps. Channel and handler removal stay with the owning step goroutine; this
// is used by Abort to invoke producer-stop cleanups from a non-step goroutine.
func (m *subscribeContext) snapshotSubscriptions() []*subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.byTopic) == 0 {
		return nil
	}
	out := make([]*subscription, 0, len(m.byTopic))
	for _, sub := range m.byTopic {
		out = append(out, sub)
	}
	return out
}

// subscription links a topic to a channel.
type subscription struct {
	cleanup     func()
	channel     *Channel
	topic       string
	id          uint64
	gen         atomic.Uint64
	cleanupOnce sync.Once
}

func (s *subscription) callCleanup() {
	if s == nil {
		return
	}
	s.cleanupOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
			s.cleanup = nil
		}
	})
}

// SubscriptionFrame carries process-epoch, subscription-id, and generation
// guards for producer frames delivered through the normal subscription path.
type SubscriptionFrame struct {
	Payloads payload.Payloads
	Epoch    uint64
	SubID    uint64
	Gen      uint64
}

func NewSubscriptionFramePayload(f *SubscriptionFrame) payload.Payload {
	return payload.NewPayload(f, payload.Golang)
}

func subscriptionFrameFromPayloads(payloads payload.Payloads) (*SubscriptionFrame, bool) {
	if len(payloads) == 0 || payloads[0] == nil {
		return nil, false
	}
	frame, ok := payloads[0].Data().(*SubscriptionFrame)
	return frame, ok
}

// SubscribeRequest is yielded to request a topic subscription.
// If ExistingChannel is nil, subscription creates the channel.
// If ExistingChannel is set, it is used instead (for externally-owned channels).
type SubscribeRequest struct {
	Handler         TopicHandler
	ExistingChannel *Channel
	Topic           string
	BufSize         int
}

func (r *SubscribeRequest) String() string       { return "<subscribe_request>" }
func (r *SubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// UnsubscribeRequest is yielded to unsubscribe a channel.
type UnsubscribeRequest struct {
	Channel *Channel
}

func (r *UnsubscribeRequest) String() string       { return "<unsubscribe_request>" }
func (r *UnsubscribeRequest) Type() lua.LValueType { return lua.LTUserData }
