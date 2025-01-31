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

type op struct {
	topic string
	unsub bool
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
		s.messageQueue.PushBack(&op{topic: topic, value: v})
	}
	if s.tg != nil {
		s.tg.WakeUp()
	}
	s.mu.Unlock()
}

// Release removes a subscription from the topic if such exists and closes attached channel. Does not
// remove messages from the queue, they can be re-consumed. Messages send prior to the release will be
// delivered to the channel.
func (s *Layer) Release(topic string) {
	s.mu.Lock()
	s.messageQueue.PushBack(&op{topic: topic, unsub: true})
	if s.tg != nil {
		s.tg.WakeUp()
	}
	s.mu.Unlock()
}

func (s *Layer) Slots(topic string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, exists := s.subs.get(topic)
	if !exists {
		return 0, fmt.Errorf("no subscribers for topic %s", topic)
	}

	return sub.channel.Slots(), nil
}

func (s *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	processableTasks := tasks
	var outTasks []*engine.Task

	boot := true
	for len(processableTasks) > 0 || boot {
		if boot {
			boot = false

			// Process message queue in boot stage
			s.mu.Lock()
			messages := make(map[*subscription][]lua.LValue)

			for e := s.messageQueue.Front(); e != nil; {
				msg := e.Value.(*op)
				nextElem := e.Next()

				if sub, exists := s.subs.get(msg.topic); exists {
					if msg.unsub {
						if err := s.channels.Close(cvm.State().Context(), sub.channel); err != nil {
							s.mu.Unlock()
							return nil, fmt.Errorf("close error: %w", err)
						}

						// we are fine to ignore this error since channel might not be subscribed yet
						_ = s.subs.remove(sub.channel)
					} else {
						messages[sub] = append(messages[sub], msg.value)
						if len(messages[sub]) > 0 {

							if err := s.channels.Send(cvm.State().Context(), sub.channel, messages[sub]...); err != nil {
								s.mu.Unlock()
								return nil, fmt.Errorf("send error: %w", err)
							}
							messages[sub] = nil
						}
					}
					s.messageQueue.Remove(e)
				}
				e = nextElem
			}
			s.mu.Unlock()
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
					if err := s.channels.Close(cvm.State().Context(), req.channel); err != nil {
						task.RaiseError = err
					} else {
						task.Resumed = []lua.LValue{lua.LTrue}
					}
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
