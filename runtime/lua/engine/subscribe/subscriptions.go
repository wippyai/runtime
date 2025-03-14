package subscribe

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
)

type subscription struct {
	topic   string
	channel *channel.Channel
}

// This context is expected to be accessed concurrently.
type subscriptionContext struct {
	byTopic   map[string]*subscription
	byChannel map[*channel.Channel]string
	mu        sync.RWMutex
}

func newSubscriptionContext() *subscriptionContext {
	return &subscriptionContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*channel.Channel]string),
	}
}

func (m *subscriptionContext) add(topic string, ch *channel.Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, fmt.Errorf("topic %q already has an active subscription", topic)
		}
		return existing, nil
	}

	sub := &subscription{
		topic:   topic,
		channel: ch,
	}
	m.byTopic[topic] = sub
	m.byChannel[ch] = topic

	return sub, nil
}

func (m *subscriptionContext) remove(ch *channel.Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	topic, exists := m.byChannel[ch]
	if !exists {
		return fmt.Errorf("channel not found in subscriptions")
	}

	delete(m.byTopic, topic)
	delete(m.byChannel, ch)
	return nil
}

func (m *subscriptionContext) get(topic string) (*subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sub, exists := m.byTopic[topic]
	return sub, exists
}
