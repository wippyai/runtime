package engine

import (
	"context"
	lua "github.com/yuin/gopher-lua"
)

type contextKey struct{}

var taskGroupKey = &contextKey{}

// TaskGroup manages a group of related tasks, their states, and result collection
type TaskGroup struct {
	results   chan TaskResult
	taskCount int32
	states    map[*lua.LState]struct{}
}

// NewTaskGroup creates a new TaskGroup instance
func NewTaskGroup(size int) *TaskGroup {
	return &TaskGroup{
		results: make(chan TaskResult, size),
		states:  make(map[*lua.LState]struct{}),
	}
}

// WithTaskGroup attaches a task group to the context
func WithTaskGroup(ctx context.Context, group *TaskGroup) context.Context {
	return context.WithValue(ctx, taskGroupKey, group)
}

// GetTaskGroup retrieves the TaskGroup from a context
func GetTaskGroup(ctx context.Context) *TaskGroup {
	if group, ok := ctx.Value(taskGroupKey).(*TaskGroup); ok {
		return group
	}
	return nil
}

// Send pushes a result into the group's channel, thread safe
func (g *TaskGroup) Send(ctx context.Context, result TaskResult) error {
	select {
	case g.results <- result:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Add registers a new Lua state for tracking
// This is always called synchronously from the main thread
func (g *TaskGroup) Add(state *lua.LState) {
	g.taskCount++
	g.states[state] = struct{}{}
}

// GetTaskCount returns the current number of tasks
func (g *TaskGroup) GetTaskCount() int32 {
	return g.taskCount
}

// wait processes all available results and returns tasks ready for resumption
func (g *TaskGroup) wait(ctx context.Context, cvm CVM, block bool) ([]*Task, error) {
	tasks := make([]*Task, 0)

	// Process all available results
	for g.taskCount > 0 {
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
				g.taskCount--
				delete(g.states, result.State)
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
			g.taskCount--
			delete(g.states, result.State)
		default:
			return tasks, nil
		}
	}

	return tasks, nil
}

// processResult converts a TaskResult into a Task ready for resumption
func (g *TaskGroup) processResult(cvm CVM, result TaskResult) (*Task, error) {
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
	if g.taskCount == 0 {
		return
	}

	g.taskCount = 0
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
