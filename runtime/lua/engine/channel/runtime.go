package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type VM interface {
	Step(tasks ...*engine.Task) ([]*engine.Task, error)
}

// Runtime coordinates task execution and channel operations
type Runtime struct {
	controller *Scheduler
}

// NewRuntime creates a new scheduler instance
func NewRuntime() *Runtime {
	return &Runtime{
		controller: NewScheduler(),
	}
}

// Step processes tasks and handles yielded operations
func (s *Runtime) Step(vm VM, tasks ...*engine.Task) ([]*engine.Task, error) {
	vmTasks, err := vm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	var externalTasks []*engine.Task
	var channelTasks []*engine.Task

	// Keep processing until all channel operations are handled
	for len(vmTasks) > 0 {
		// Process current batch of tasks through controller
		processedTasks, err := s.controller.HandleChannelTasks(vmTasks)
		if err != nil {
			return nil, fmt.Errorf("task processing failed: %w", err)
		}

		// Separate channel tasks from external tasks
		externalTasks = append(externalTasks, s.filterExternalTasks(processedTasks)...)
		channelTasks = s.filterChannelTasks(processedTasks)

		if len(channelTasks) == 0 {
			break
		}

		// Continue processing channel tasks
		vmTasks, err = vm.Step(channelTasks...)
		if err != nil {
			return nil, fmt.Errorf("coroutine failed: %w", err)
		}
	}

	return externalTasks, nil
}

// ActiveSignals returns list of active inbox channels
func (s *Runtime) ActiveSignals() []string {
	return s.controller.GetActiveSignals()
}

// Send sends a value to a named inbox channel
func (s *Runtime) Send(name string, value lua.LValue) []*engine.Task {
	return s.controller.Send(name, value)
}

// filterExternalTasks separates non-channel tasks
func (s *Runtime) filterExternalTasks(tasks []*engine.Task) []*engine.Task {
	var external []*engine.Task
	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			external = append(external, task)
			continue
		}

		switch task.Yielded[0].(type) {
		case *chanOperation, *selectSwitch:
			continue
		default:
			external = append(external, task)
		}
	}
	return external
}

// filterChannelTasks separates channel-related tasks
func (s *Runtime) filterChannelTasks(tasks []*engine.Task) []*engine.Task {
	var channel []*engine.Task
	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			continue
		}

		switch task.Yielded[0].(type) {
		case *chanOperation, *selectSwitch:
			channel = append(channel, task)
		}
	}
	return channel
}

// Cleanup releases scheduler resources
func (s *Runtime) Cleanup() {
	s.controller.Cleanup()
}
