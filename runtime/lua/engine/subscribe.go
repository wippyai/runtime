package engine

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// subscribeContext manages topic-to-channel mappings.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	mu        sync.RWMutex
}

func (m *subscribeContext) add(topic string, ch *Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, luaapi.NewTopicAlreadySubscribedError(topic)
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

// subscription links a topic to a channel.
type subscription struct {
	topic   string
	channel *Channel
}

// SubscribeRequest is yielded to request a topic subscription.
type SubscribeRequest struct {
	Topic   string
	Channel *Channel
	Handler TopicHandler
}

func (r *SubscribeRequest) String() string       { return "<subscribe_request>" }
func (r *SubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// UnsubscribeRequest is yielded to unsubscribe a channel.
type UnsubscribeRequest struct {
	Channel *Channel
}

func (r *UnsubscribeRequest) String() string       { return "<unsubscribe_request>" }
func (r *UnsubscribeRequest) Type() lua.LValueType { return lua.LTUserData }
