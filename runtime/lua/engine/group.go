package engine

import (
	"context"
	"errors"
	"sync/atomic"

	capi "github.com/ponyruntime/pony/api/context"
	lua "github.com/yuin/gopher-lua"
)

// TaskGroup manages a group of related tasks, their states, and result collection
type TaskGroup struct {
	results    chan *Result
	wakeup     chan struct{}
	wakeCount  atomic.Int32
	taskCount  atomic.Int32
	awaken     atomic.Bool
	wakeupFunc func()
	states     map[*lua.LState]struct{}
}

// NewTaskGroup creates a new TaskGroup instance
func NewTaskGroup(size int) *TaskGroup {
	return &TaskGroup{
		results: make(chan *Result, size),
		wakeup:  make(chan struct{}, size),
		states:  make(map[*lua.LState]struct{}),
	}
}

// WithTaskGroup attaches a task group to the context
func WithTaskGroup(ctx context.Context, group *TaskGroup) context.Context {
	return context.WithValue(ctx, capi.RunnerCtx, group)
}

// GetTaskGroup retrieves the TaskGroup from a context
func GetTaskGroup(ctx context.Context) *TaskGroup {
	if group, ok := ctx.Value(capi.RunnerCtx).(*TaskGroup); ok {
		return group
	}
	return nil
}

// Send pushes a result into the group's channel, thread safe
func (g *TaskGroup) Send(ctx context.Context, result *Result) error {
	select {
	case g.results <- result:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Add registers a new Lua state for tracking, not thread safe
// This is always called synchronously from the main thread
func (g *TaskGroup) Add(state *lua.LState) {
	g.taskCount.Add(1)
	if state != nil {
		g.states[state] = struct{}{} // make th read safe
	}
}

// Remove unregisters a Lua state from tracking, not thread safe
func (g *TaskGroup) Remove(state *lua.LState) {
	g.taskCount.Add(^(int32(0)))
	if state != nil {
		delete(g.states, state) // make th read safe
	}
}

// WakeUp increments the wake count and sends a wakeup signal, thread safe
func (g *TaskGroup) WakeUp() {
	if g.awaken.CompareAndSwap(false, true) {
		// we only call the wakeup function once per wait cycle to ensure that no tasks are missed
		// never allow situation when
		if g.wakeupFunc != nil {
			g.wakeupFunc()
		}

		g.wakeCount.Add(1)
		select {
		case g.wakeup <- struct{}{}:
		default:
		}
	}
}

// GetTaskCount returns the current number of tasks
func (g *TaskGroup) GetTaskCount() int {
	return int(g.taskCount.Load())
}

// Wait processes all available results and returns tasks ready for resumption
func (g *TaskGroup) Wait(ctx context.Context, cvm CVM, block bool) ([]*Task, error) {
	defer g.awaken.Store(false)

	tasks := make([]*Task, 0)

	// Process all available results
	for g.taskCount.Load() > 0 {
		if block {
			select {
			case result := <-g.results:
				task, err := g.processResult(cvm, result)
				if err != nil {
					return nil, err
				}
				if task != nil {
					tasks = append(tasks, task)
				}
				g.taskCount.Add(^int32(0))

				delete(g.states, result.State)
				block = false
				continue
			case <-g.wakeup:
				g.wakeCount.Add(^int32(0))
				g.awaken.Store(false)

				// WakeUp up and continue processing
				block = false
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Non-blocking check for more results
		select {
		case result := <-g.results:
			task, err := g.processResult(cvm, result)
			if err != nil {
				return nil, err
			}
			if task != nil {
				tasks = append(tasks, task)
			}
			g.taskCount.Add(^int32(0))

			delete(g.states, result.State)
		case <-g.wakeup:
			g.wakeCount.Add(^int32(0))
			g.awaken.Store(false)

			// WakeUp up and continue processing
		default:
			return tasks, nil
		}
	}

	return tasks, nil
}

// processResult converts a Result into a Task ready for resumption
func (g *TaskGroup) processResult(cvm CVM, result *Result) (*Task, error) {
	if result == nil {
		return nil, errors.New("nil result received")
	}

	task, err := cvm.GetTask(result.State)
	if err != nil {
		return nil, err
	}

	if result.Error != nil {
		task.RaiseError = result.Error
	} else {
		task.Resumed = result.Result
	}

	return task, nil
}

func (g *TaskGroup) clean() {
	if g.taskCount.Load() == 0 {
		return
	}

	g.taskCount.Store(0)
	g.wakeCount.Store(0)
	g.states = make(map[*lua.LState]struct{})

	// drain
	ok := true
	for {
		if !ok {
			break
		}

		select {
		case _, okD := <-g.results:
			if !okD {
				ok = false
			}
		default:
			ok = false
		}
	}
}

// GetActiveStates returns a slice of currently active Lua states
func (g *TaskGroup) GetActiveStates() []*lua.LState {
	states := make([]*lua.LState, 0, len(g.states))
	for state := range g.states {
		states = append(states, state)
	}
	return states
}

// HasState checks if a specific Lua state is currently active
func (g *TaskGroup) HasState(state *lua.LState) bool {
	_, exists := g.states[state]
	return exists
}
