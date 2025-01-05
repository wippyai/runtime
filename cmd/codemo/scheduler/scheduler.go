package scheduler

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

type Task struct {
	l           *lua.LState
	thread      *lua.LState
	state       lua.ResumeState
	yieldedVals []lua.LValue
	resumeVal   lua.LValue
	cancel      context.CancelFunc
}

func (t *Task) IsYielded() bool {
	return t.state == lua.ResumeYield
}

func (t *Task) GetYieldedValues() []lua.LValue {
	return t.yieldedVals
}

func (t *Task) SetResumeValue(val lua.LValue) {
	t.resumeVal = val
}

type Scheduler struct {
	vm    *VM
	tasks []*Task
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks: []*Task{},
	}
}

func (s *Scheduler) Attach(ctx context.Context, vm *VM) {
	s.vm = vm
	s.vm.State().SetContext(ctx)
	// Set up scheduler global
	s.setupGlobals()
}

func (s *Scheduler) setupGlobals() {
	state := s.vm.State()
	schedulerTable := state.NewTable()
	state.SetGlobal("scheduler", schedulerTable)

	state.SetField(schedulerTable, "createCoroutine", state.NewFunction(func(L *lua.LState) int {
		fn := L.CheckFunction(1)
		_, err := s.AddTask(fn)
		if err != nil {
			L.RaiseError("failed to add task: %v", err)
		}
		return 0
	}))
}

func (s *Scheduler) DoString(code string) error {
	return s.vm.State().DoString(code)
}

func (s *Scheduler) AddTask(fn *lua.LFunction) (*Task, error) {
	thread, cancel := s.vm.State().NewThread()

	task := &Task{
		l:      s.vm.State(),
		thread: thread,
		cancel: cancel,
	}

	state, err, values := s.vm.State().Resume(thread, fn)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("error starting coroutine: %v", err)
	}

	task.state = state
	task.yieldedVals = values

	s.tasks = append(s.tasks, task)
	return task, nil
}

func (s *Scheduler) Step(tasks ...*Task) ([]*Task, error) {
	newlyYielded := make([]*Task, 0)

	for _, task := range tasks {
		if task.state != lua.ResumeYield {
			continue
		}

		state, err, values := s.vm.State().Resume(task.thread, nil, task.resumeVal)
		if err != nil {
			return nil, fmt.Errorf("error resuming task: %v", err)
		}

		task.state = state
		task.yieldedVals = values
		task.resumeVal = nil

		if state == lua.ResumeYield {
			newlyYielded = append(newlyYielded, task)
		}
	}

	return newlyYielded, nil
}

func (s *Scheduler) GetYieldedTasks() []*Task {
	yielded := make([]*Task, 0)
	for _, task := range s.tasks {
		if task.state == lua.ResumeYield {
			yielded = append(yielded, task)
		}
	}
	return yielded
}

func (s *Scheduler) RemoveTask(task *Task) error {
	for i, t := range s.tasks {
		if t == task {
			if task.cancel != nil {
				task.cancel()
			}
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task not found")
}

func (s *Scheduler) Close() error {
	for _, task := range s.tasks {
		if task.cancel != nil {
			task.cancel()
		}
	}
	s.tasks = nil
	return nil
}
