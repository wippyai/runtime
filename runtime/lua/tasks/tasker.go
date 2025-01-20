package tasks

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync/atomic"
)

// Result represents possible outputs from task execution
type Result struct {
	Values []lua.LValue
	Error  error
}

// taskSchedule represents a message that can be sent through the tasker
type taskSchedule struct {
	id      string
	input   lua.LValue
	channel chan *Result
}

// Tasker manages task execution within a Lua VM
type Tasker struct {
	cvm     *engine.CoroutineVM
	wrapped *engine.CVMWrapper
	inbox   chan *taskSchedule
	running atomic.Bool
}

// NewTasker creates a new instance of the task manager
func NewTasker(cvm *engine.CoroutineVM, channels *channel.Runner, inboxSize int, opts ...engine.CVMOption) (*Tasker, error) {
	// Create task mixer
	mixer := NewMixer(channels, inboxSize)

	// Set up base layers and add user options
	baseOpts := []engine.CVMOption{
		engine.WithLayer(channels),
		engine.WithLayer(mixer),
	}

	// Append any additional user options
	opts = append(baseOpts, opts...)

	// Create wrapped VM with all layers
	wrapped := engine.NewWrappedCVM(cvm, opts...)

	tasker := &Tasker{
		cvm:     cvm,
		wrapped: wrapped,
		inbox:   make(chan *taskSchedule, inboxSize),
	}

	return tasker, nil
}

// Start initiates the task manager service
func (t *Tasker) Start(ctx context.Context) (<-chan any, error) {
	if !t.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("tasker already running")
	}

	status := make(chan any, 1)

	// Start the main task processing loop
	go func() {
		defer close(status)
		defer t.running.Store(false)

		// Import and start the task handler
		err := t.cvm.Import(`
			function task_handler()
				local inbox = tasks.channel()
				
				while true do
					local task, ok = inbox:receive()
					if not ok then
						break
					end
					
					task:complete(task:input())
				end
				
				return "exit"
			end
		`, "tasks", "task_handler")

		if err != nil {
			status <- fmt.Sprintf("failed to import handler: %v", err)
			return
		}

		// Execute the handler
		done := make(chan struct{})
		go func() {
			defer close(done)
			result, err := t.wrapped.Execute(ctx, "task_handler")
			if err != nil {
				status <- fmt.Sprintf("handler error: %v", err)
				return
			}
			status <- fmt.Sprintf("handler exit: %v", result)
		}()

		// Process incoming tasks
		for {
			select {
			case <-ctx.Done():
				close(t.inbox)
				<-done // Wait for handler to exit
				return

			case schedule, ok := <-t.inbox:
				if !ok {
					<-done // Wait for handler to exit
					return
				}

				go func(schedule *taskSchedule) {
					out, err := t.wrapped.Send(ctx, schedule.id, schedule.input)
					if err != nil {
						schedule.channel <- &Result{Error: err}
						close(schedule.channel)
						return
					}

					select {
					case result := <-out:
						schedule.channel <- &Result{
							Values: result.Values,
							Error:  result.Error,
						}
						close(schedule.channel)
					case <-ctx.Done():
						schedule.channel <- &Result{
							Error: ctx.Err(),
						}
						close(schedule.channel)
					}
				}(schedule)
			}
		}
	}()

	return status, nil
}

// Stop gracefully shuts down the task manager
func (t *Tasker) Stop(ctx context.Context) error {
	if !t.running.Load() {
		return nil
	}

	// Close inbox to signal shutdown
	close(t.inbox)

	// Wait for context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// Schedule submits a new task for execution
func (t *Tasker) Schedule(id string, input lua.LValue) (<-chan *Result, error) {
	if !t.running.Load() {
		return nil, fmt.Errorf("tasker not running")
	}

	resultChan := make(chan *Result, 1)
	schedule := &taskSchedule{
		id:      id,
		input:   input,
		channel: resultChan,
	}

	// Try to send task
	select {
	case t.inbox <- schedule:
		return resultChan, nil
	default:
		close(resultChan)
		return nil, fmt.Errorf("task queue full")
	}
}
