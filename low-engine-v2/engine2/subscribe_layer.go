package engine2

import (
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	lua "github.com/yuin/gopher-lua"
)

// SubscribeLayerKey is the context key for subscribe layer state.
var SubscribeLayerKey = &ctxapi.Key{Name: "engine2.subscribe_layer", Inherit: false}

// subscription links a topic to a channel.
type subscription struct {
	topic   string
	channel *Channel
}

// subscribeContext manages topic-to-channel mappings.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	mu        sync.RWMutex
}

func newSubscribeContext() *subscribeContext {
	return &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}
}

func (m *subscribeContext) add(topic string, ch *Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, fmt.Errorf("topic %q already subscribed", topic)
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
		return fmt.Errorf("channel not found")
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

// subscribeLayerContext holds per-process subscribe state.
type subscribeLayerContext struct {
	subs *subscribeContext
}

// SubscribeLayer handles external message routing to internal channels.
type SubscribeLayer struct{}

// NewSubscribeLayer creates a new subscribe layer.
func NewSubscribeLayer() *SubscribeLayer {
	return &SubscribeLayer{}
}

// Step processes incoming messages and handles subscribe/unsubscribe yields.
func (l *SubscribeLayer) Step(proc *Process, tasks ...*Task) ([]*Task, error) {
	lctx := l.ensureContext(proc)
	if lctx == nil {
		return nil, fmt.Errorf("subscribe layer context not found")
	}

	// Route incoming messages from Resources to subscribed channels
	res := GetResources(proc.ctx)
	if res != nil {
		for _, pkg := range res.DrainMessages() {
			for _, msg := range pkg.Messages {
				topic := string(msg.Topic)
				if sub, exists := lctx.subs.get(topic); exists {
					value := messageToLua(proc.state, msg)
					result := sub.channel.Send(nil, value, nil)

					// Wake any blocked receivers
					updates := result.GetUpdates()
					if result.Yields && len(updates) > 0 {
						for _, upd := range updates {
							if upd.State == nil {
								continue
							}
							t, err := proc.GetTask(upd.State)
							if err == nil {
								t.Resumed = upd.GetResult()
								proc.queue.Push(t)
							}
						}
					}
				}
			}
		}
	}

	// Handle subscribe/unsubscribe yields from incoming tasks
	var outTasks []*Task
	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			outTasks = append(outTasks, task)
			continue
		}

		lastYield := task.Yielded[len(task.Yielded)-1]

		// Handle subscribe request
		if req, ok := lastYield.(*SubscribeRequest); ok {
			sub, err := lctx.subs.add(req.Topic, req.Channel)
			if err != nil {
				task.Resumed = []lua.LValue{lua.LNil, lua.LString(err.Error())}
			} else {
				task.Resumed = []lua.LValue{sub.channel.Value()}
			}
			proc.queue.Push(task)
			continue
		}

		// Handle unsubscribe request
		if req, ok := lastYield.(*UnsubscribeRequest); ok {
			err := lctx.subs.remove(req.Channel)
			if err != nil {
				task.Resumed = []lua.LValue{lua.LFalse, lua.LString(err.Error())}
			} else {
				req.Channel.Close()
				task.Resumed = []lua.LValue{lua.LTrue}
			}
			proc.queue.Push(task)
			continue
		}

		outTasks = append(outTasks, task)
	}

	return outTasks, nil
}

// ensureContext gets or creates the subscribe layer context.
func (l *SubscribeLayer) ensureContext(proc *Process) *subscribeLayerContext {
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return nil
	}

	if val, ok := fc.Get(SubscribeLayerKey); ok {
		return val.(*subscribeLayerContext)
	}

	lctx := &subscribeLayerContext{
		subs: newSubscribeContext(),
	}
	fc.Set(SubscribeLayerKey, lctx)
	return lctx
}

// HasSubscriptions returns true if the process has any active subscriptions.
func HasSubscriptions(proc *Process) bool {
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return false
	}

	val, ok := fc.Get(SubscribeLayerKey)
	if !ok {
		return false
	}

	lctx := val.(*subscribeLayerContext)
	lctx.subs.mu.RLock()
	defer lctx.subs.mu.RUnlock()
	return len(lctx.subs.byTopic) > 0
}

// SubscribeRequest is yielded to request a topic subscription.
type SubscribeRequest struct {
	Topic   string
	Channel *Channel
}

func (r *SubscribeRequest) String() string       { return "<subscribe_request>" }
func (r *SubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// UnsubscribeRequest is yielded to unsubscribe a channel.
type UnsubscribeRequest struct {
	Channel *Channel
}

func (r *UnsubscribeRequest) String() string       { return "<unsubscribe_request>" }
func (r *UnsubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// messageToLua converts a relay.Message to Lua value.
func messageToLua(l *lua.LState, msg interface{}) lua.LValue {
	// Simplified: extract payload data
	// Full implementation would convert payloads properly
	return lua.LString(fmt.Sprintf("%v", msg))
}
