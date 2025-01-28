package command

import (
	"container/list"
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync"
)

type commandContextKey struct{}

var cmdCtxKey = &commandContextKey{}

// Layer implements command scheduling and execution
type Layer struct {
	mu       sync.Mutex
	channels *channel.Layer
	pending  *list.List // Commands waiting to be processed
	results  *list.List // Completed commands with results to send
}

// NewCommandLayer creates a new command processing layer
func NewCommandLayer(channels *channel.Layer) *Layer {
	return &Layer{
		channels: channels,
		pending:  list.New(),
		results:  list.New(),
	}
}

// WithContext adds layer's scheduling context
func (l *Layer) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, cmdCtxKey, l)
}

// GetCommandContext retrieves the command layer from context
func GetCommandContext(ctx context.Context) *Layer {
	if ctx == nil {
		return nil
	}

	if l, ok := ctx.Value(cmdCtxKey).(*Layer); ok {
		return l
	}

	return nil
}

// Schedule adds a command to pending queue
func (l *Layer) Schedule(cmd *Command) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pending.PushBack(cmd)
}

// QueueResult queues a completed command for sending result
func (l *Layer) QueueResult(cmd *Command, result lua.LValue) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !cmd.IsComplete() {
		cmd.SetResult(result)
		l.results.PushBack(cmd)
	}
}

// QueueError queues a failed command for sending error
func (l *Layer) QueueError(cmd *Command, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !cmd.IsComplete() {
		cmd.SetError(err)
		l.results.PushBack(cmd)
	}
}

// GetPendingCommands extracts all pending commands
func (l *Layer) GetPendingCommands() []*Command {
	l.mu.Lock()
	defer l.mu.Unlock()

	var commands []*Command
	for e := l.pending.Front(); e != nil; e = e.Next() {
		if cmd, ok := e.Value.(*Command); ok {
			commands = append(commands, cmd)
		}
	}
	l.pending.Init() // Clear processed
	return commands
}

// Step implements the engine.Layer interface
func (l *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	l.mu.Lock()

	// First process any queued results
	for e := l.results.Front(); e != nil; {
		cmd := e.Value.(*Command)
		nextElem := e.Next() // Store next before removal
		l.results.Remove(e)  // Remove returns the element but we already have cmd

		ctx := l.getSharedContext(cvm)

		if cmd.err != nil {
			// Send error by closing channel
			if err := l.channels.Close(ctx, cmd.response); err != nil {
				l.mu.Unlock()
				return nil, fmt.Errorf("close error: %w", err)
			}
		} else {
			// Send success result
			if err := l.channels.Send(ctx, cmd.response, cmd.result); err != nil {
				l.mu.Unlock()
				return nil, fmt.Errorf("send error: %w", err)
			}

			// And close channel
			if err := l.channels.Close(ctx, cmd.response); err != nil {
				l.mu.Unlock()
				return nil, fmt.Errorf("close error: %w", err)
			}
		}
		e = nextElem
	}
	l.mu.Unlock()

	// Process tasks through chain
	outTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, fmt.Errorf("step error: %w", err)
	}

	// Check for command yields
	var processableTasks []*engine.Task
	for _, task := range outTasks {
		if len(task.Yielded) == 0 {
			processableTasks = append(processableTasks, task)
			continue
		}

		// Check if last yield is a command
		if cmd, ok := isCommand(task.Yielded[len(task.Yielded)-1]); ok {
			// Schedule using layer directly since we're in Step
			l.Schedule(cmd)
			continue
		}

		processableTasks = append(processableTasks, task)
	}

	return processableTasks, nil
}

func (l *Layer) getSharedContext(cvm engine.CVM) context.Context {
	for _, task := range cvm.GetTasks() {
		if task.Thread().Context() != nil {
			return task.Thread().Context()
		}
	}

	return nil
}

// isCommand checks if a value is a Command instance
func isCommand(v lua.LValue) (*Command, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if cmd, ok := ud.Value.(*Command); ok {
			return cmd, true
		}
	}

	return nil, false
}
