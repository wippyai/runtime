package pubsub

import (
	"container/list"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/net/context"
	"sync"
)

type sendMessage struct {
	topic string
	value lua.LValue
}

type Layer struct {
	mu           sync.Mutex
	tg           *engine.TaskGroup
	channels     *channel.Layer
	subs         *subscriptionManager
	messageQueue *list.List
}

func NewSubscriptionLayer(channels *channel.Layer) *Layer {
	return &Layer{
		channels:     channels,
		subs:         newSubscriptionManager(),
		messageQueue: list.New(),
	}
}

func (s *Layer) WithContext(ctx context.Context) context.Context {
	s.tg = engine.GetTaskGroup(ctx)
	return ctx
}

func (s *Layer) Publish(topic string, value ...lua.LValue) {
	s.mu.Lock()
	for _, v := range value {
		s.messageQueue.PushBack(&sendMessage{topic: topic, value: v})
	}
	if s.tg != nil {
		s.tg.WakeUp()
	}
	s.mu.Unlock()
}

func (s *Layer) Slots(topic string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, exists := s.subs.get(topic)
	if !exists {
		return 0, fmt.Errorf("no subscribers for topic %s", topic)
	}

	return ch.channel.Slots(), nil
}

func (s *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	processableTasks := tasks
	var outTasks []*engine.Task

	// Process messages
	pendingWrites := s.processMessages()

	// Process tasks with writes and yields in a loop
	boot := true
	for len(processableTasks) > 0 || boot {
		boot = false

		// Write messages to channels
		for sub, messages := range pendingWrites {
			if len(messages) > 0 {
				// todo: we can add some backpressure management here
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
		hasYields := false
		for _, task := range nextTasks {
			if len(task.Yielded) == 0 {
				outTasks = append(outTasks, task)
				continue
			}

			lastYield := task.Yielded[len(task.Yielded)-1]

			// Handle subscription requests
			if req, ok := isSubscriptionRequest(lastYield); ok {
				hasYields = true
				s.mu.Lock()
				sub, err := s.subs.add(req.topic, req.channel)
				s.mu.Unlock()

				if err != nil {
					task.RaiseError = err
				} else {
					task.Resumed = []lua.LValue{channel.Wrap(task.Thread(), sub.channel)}
				}

				processableTasks = append(processableTasks, task)
				continue
			}

			// Handle unsubscribe requests
			if req, ok := isUnsubscribeRequest(lastYield); ok {
				hasYields = true
				s.mu.Lock()
				err := s.subs.remove(req.channel)
				s.mu.Unlock()

				if err != nil {
					task.RaiseError = err
				} else {
					task.Resumed = []lua.LValue{lua.LTrue}
				}

				processableTasks = append(processableTasks, task)
				continue
			}

			outTasks = append(outTasks, task)
		}

		if !hasYields {
			break
		}
	}

	return outTasks, nil
}

func (s *Layer) processMessages() map[*subscription][]lua.LValue {
	pendingWrites := make(map[*subscription][]lua.LValue)

	s.mu.Lock()
	defer s.mu.Unlock()

	for e := s.messageQueue.Front(); e != nil; {
		msg := e.Value.(*sendMessage)
		nextElem := e.Next()

		if sub, exists := s.subs.get(msg.topic); exists {
			pendingWrites[sub] = append(pendingWrites[sub], msg.value)
			s.messageQueue.Remove(e)
		}
		e = nextElem
	}

	return pendingWrites
}

func isSubscriptionRequest(v lua.LValue) (*subscribe, bool) {
	if req, ok := v.(*subscribe); ok {
		return req, true
	}
	return nil, false
}

func isUnsubscribeRequest(v lua.LValue) (*unsubscribe, bool) {
	if req, ok := v.(*unsubscribe); ok {
		return req, true
	}
	return nil, false
}
