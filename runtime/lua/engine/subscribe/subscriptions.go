package subscribe

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
)

type subscription struct {
	topic   string
	channel *channel.Channel
}

// NOT thread safe, use with external sync.
type subscriptionManager struct {
	byTopic   map[string]*subscription
	byChannel map[*channel.Channel]string
}

func newSubscriptionManager() *subscriptionManager {
	return &subscriptionManager{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*channel.Channel]string),
	}
}

func (m *subscriptionManager) add(topic string, ch *channel.Channel) (*subscription, error) {
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

func (m *subscriptionManager) remove(ch *channel.Channel) error {
	topic, exists := m.byChannel[ch]
	if !exists {
		return fmt.Errorf("channel not found in subscriptions")
	}

	delete(m.byTopic, topic)
	delete(m.byChannel, ch)
	return nil
}

func (m *subscriptionManager) get(topic string) (*subscription, bool) {
	sub, exists := m.byTopic[topic]
	return sub, exists
}
