package pubsub

import (
	"container/list"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync"
)

// Subscribe represents a yield request for a subscription
type Subscribe struct {
	topic string
}

func (r *Subscribe) String() string {
	return fmt.Sprintf("subscription.request{topic=%s}", r.topic)
}

func (r *Subscribe) Type() lua.LValueType {
	return lua.LTUserData
}

func NewRequest(topic string) *Subscribe {
	return &Subscribe{topic: topic}
}

// subscription tracks an active subscription
type subscription struct {
	topic   string
	channel *channel.Channel
	refs    int
}

// pendingMessage represents a message waiting to be sent
type pendingMessage struct {
	topic string
	value lua.LValue
}

// Layer manages topic subscriptions
type Layer struct {
	mu           sync.Mutex
	channels     *channel.Layer
	subscribers  map[string]*subscription
	messageQueue *list.List
}

func NewSubscriptionLayer(channels *channel.Layer) *Layer {
	return &Layer{
		channels:     channels,
		subscribers:  make(map[string]*subscription),
		messageQueue: list.New(),
	}
}

func (s *Layer) Publish(topic string, value lua.LValue) {
	s.mu.Lock()
	s.messageQueue.PushBack(&pendingMessage{
		topic: topic,
		value: value,
	})
	s.mu.Unlock()
}

// Step implements the engine.Layer interface
func (s *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	processableTasks := tasks
	var outTasks []*engine.Task

	// 1. Prepare messages to write, grouping by subscription channel (we only write messages arrived before this step)
	pendingWrites := make(map[*subscription][]lua.LValue)
	s.mu.Lock()
	for e := s.messageQueue.Front(); e != nil; {
		msg := e.Value.(*pendingMessage)
		nextElem := e.Next()

		if sub, exists := s.subscribers[msg.topic]; exists {
			// Group messages by subscription channel
			if len(pendingWrites[sub]) < sub.channel.Slots() {
				pendingWrites[sub] = append(pendingWrites[sub], msg.value)
				s.messageQueue.Remove(e)
			}
		}
		e = nextElem
	}
	s.mu.Unlock()

	// Process tasks with writes and yields in a loop
	boot := true
	for len(processableTasks) > 0 || boot {
		boot = false

		// 2. Write messages to channels and process CVM
		for sub, messages := range pendingWrites {
			if len(messages) > 0 {
				if err := s.channels.Send(cvm.State().Context(), sub.channel, messages...); err != nil {
					return nil, fmt.Errorf("send error: %w", err)
				}
			}
		}

		// Process through CVM
		nextTasks, err := cvm.Step(processableTasks...)
		if err != nil {
			return nil, err
		}

		processableTasks = nil

		// Process yields and collect tasks
		hasSubscribeYields := false
		for _, task := range nextTasks {
			if len(task.Yielded) == 0 {
				outTasks = append(outTasks, task)
				continue
			}

			// Handle subscription requests
			if req, ok := isSubscriptionRequest(task.Yielded[len(task.Yielded)-1]); ok {
				hasSubscribeYields = true
				s.mu.Lock()
				sub, err := s.getOrCreateSubscription(req.topic)
				if err != nil {
					s.mu.Unlock()
					return nil, fmt.Errorf("subscription error: %w", err)
				}

				// clients will be round-robin'd
				task.Resumed = []lua.LValue{channel.Wrap(task.Thread(), sub.channel)}
				s.mu.Unlock()

				processableTasks = append(processableTasks, task)
				continue
			}

			outTasks = append(outTasks, task)
		}

		// Continue loop only if we have subscription yields to process
		if !hasSubscribeYields {
			break
		}
	}

	return outTasks, nil
}

func (s *Layer) getOrCreateSubscription(topic string) (*subscription, error) {
	sub, exists := s.subscribers[topic]
	if exists {
		sub.refs++
		return sub, nil
	}

	// Create new subscription channel
	ch := channel.Named(fmt.Sprintf("sub.%s", topic), 1) // Always buffer size 1
	if ch == nil {
		return nil, fmt.Errorf("failed to create subscription channel")
	}

	sub = &subscription{
		topic:   topic,
		channel: ch,
		refs:    1,
	}
	s.subscribers[topic] = sub

	return sub, nil
}

func isSubscriptionRequest(v lua.LValue) (*Subscribe, bool) {
	if req, ok := v.(*Subscribe); ok {
		return req, true
	}
	return nil, false
}
