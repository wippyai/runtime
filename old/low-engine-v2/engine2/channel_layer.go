package engine2

import (
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	lua "github.com/yuin/gopher-lua"
)

// ChannelLayerKey is the context key for channel layer state in FrameContext.
var ChannelLayerKey = &ctxapi.Key{Name: "engine2.channel_layer", Inherit: false}

// channelLayerContext holds per-process channel layer state.
type channelLayerContext struct {
	queue    *TaskQueue
	channels map[*Channel]int // Track channels with reference counting
}

// ChannelLayer handles channel operations between coroutines.
type ChannelLayer struct{}

// NewChannelLayer creates a new channel layer.
func NewChannelLayer() *ChannelLayer {
	return &ChannelLayer{}
}

// Step processes tasks, handling channel operations internally.
func (l *ChannelLayer) Step(proc *Process, tasks ...*Task) ([]*Task, error) {
	lctx := l.ensureContext(proc)
	if lctx == nil {
		return nil, fmt.Errorf("channel layer context not found")
	}

	externalTasks := make([]*Task, 0)

	// Queue incoming tasks
	for _, task := range tasks {
		lctx.queue.Push(task)
	}

	// Process all queued tasks
	boot := true
	for !lctx.queue.IsEmpty() || boot {
		boot = false

		// Drain to batch
		batch := lctx.queue.Drain()

		// Run through VM step
		vmTasks, err := proc.vmStep(batch...)
		if err != nil {
			return nil, err
		}

		// Process each yielded task
		for _, task := range vmTasks {
			if len(task.Yielded) == 0 {
				continue
			}

			// Check if yield is a channel operation
			value := task.Yielded[len(task.Yielded)-1]
			result, ok := value.(*ChannelResult)
			if !ok {
				// Not a channel operation, pass to outer layer
				externalTasks = append(externalTasks, task)
				continue
			}

			// Update channel references
			l.updateRefs(lctx, result.Block, result.Release)

			// Process updates from channel operation
			updates := result.GetUpdates()
			if result.Yields && len(updates) > 0 {
				for _, upd := range updates {
					if upd.State == nil {
						continue
					}

					t, err := proc.GetTask(upd.State)
					if err != nil {
						ReleaseResult(result)
						return nil, fmt.Errorf("task not found for channel result: %w", err)
					}

					if upd.Error != nil {
						t.ResumeWith(lua.LNil, lua.LString(upd.Error.Error()))
					} else {
						t.ResumeWith(upd.GetResult()...)
					}

					lctx.queue.Push(t)
				}
			}

			ReleaseResult(result)
		}
	}

	return externalTasks, nil
}

// ensureContext gets or creates the channel layer context.
func (l *ChannelLayer) ensureContext(proc *Process) *channelLayerContext {
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return nil
	}

	if val, ok := fc.Get(ChannelLayerKey); ok {
		return val.(*channelLayerContext)
	}

	lctx := &channelLayerContext{
		queue:    NewTaskQueue(),
		channels: make(map[*Channel]int),
	}
	fc.Set(ChannelLayerKey, lctx)
	return lctx
}

// updateRefs handles reference counting for channels.
func (l *ChannelLayer) updateRefs(lctx *channelLayerContext, blocks, releases []*Channel) {
	for _, ch := range blocks {
		if _, exists := lctx.channels[ch]; !exists {
			lctx.channels[ch] = 0
		}
		lctx.channels[ch]++
	}

	for _, ch := range releases {
		if _, exists := lctx.channels[ch]; exists {
			lctx.channels[ch]--
			if lctx.channels[ch] == 0 {
				delete(lctx.channels, ch)
			}
		}
	}
}

// ActiveChannel represents a channel blocking execution.
type ActiveChannel struct {
	Name  string
	Slots int
	Refs  int
}

// GetActiveChannels returns channels currently blocking execution.
func GetActiveChannels(proc *Process) []ActiveChannel {
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return nil
	}

	val, ok := fc.Get(ChannelLayerKey)
	if !ok {
		return nil
	}

	lctx := val.(*channelLayerContext)
	result := make([]ActiveChannel, 0, len(lctx.channels))
	for ch, refs := range lctx.channels {
		result = append(result, ActiveChannel{
			Name:  ch.Name(),
			Slots: ch.Slots(),
			Refs:  refs,
		})
	}

	return result
}
