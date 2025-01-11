package channel

import (
	"errors"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

type testVM struct {
	stepFunc func(tasks ...*engine.Task) ([]*engine.Task, error)
}

func (v *testVM) Step(tasks ...*engine.Task) ([]*engine.Task, error) {
	return v.stepFunc(tasks...)
}

type testScheduler struct {
	handleTasksFunc     func(tasks []*engine.Task) ([]*engine.Task, error)
	sendFunc            func(name string, value lua.LValue) ([]*engine.Task, error)
	getOpenChannelsFunc func() []string
	closeFunc           func()
}

func (s *testScheduler) handleTasks(tasks []*engine.Task) ([]*engine.Task, error) {
	return s.handleTasksFunc(tasks)
}

func (s *testScheduler) send(name string, value lua.LValue) ([]*engine.Task, error) {
	return s.sendFunc(name, value)
}

func (s *testScheduler) getOpenChannels() []string {
	return s.getOpenChannelsFunc()
}

func (s *testScheduler) close() {
	s.closeFunc()
}

func newTestTask() *engine.Task {
	return &engine.Task{
		State:   lua.ResumeOK,
		Yielded: make([]lua.LValue, 0),
		Resumed: make([]lua.LValue, 0),
	}
}

func TestNewRuntime(t *testing.T) {
	runtime := NewRuntime()
	assert.NotNil(t, runtime)
	assert.NotNil(t, runtime.scheduler)
}

func TestRuntime_Step_Success(t *testing.T) {
	initialTask := newTestTask()
	vmTask := newTestTask()
	vmTask.Yielded = []lua.LValue{&chanOperation{}}

	var vmCalls int
	vm := &testVM{
		stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
			vmCalls++
			if vmCalls == 1 {
				return []*engine.Task{vmTask}, nil
			}
			return []*engine.Task{}, nil
		},
	}

	sh := &testScheduler{
		handleTasksFunc: func(tasks []*engine.Task) ([]*engine.Task, error) {
			return []*engine.Task{}, nil
		},
	}

	runtime := &Runtime{scheduler: sh}
	result, err := runtime.Step(vm, initialTask)

	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestRuntime_Step_VMError(t *testing.T) {
	expectedErr := errors.New("VM error")
	vm := &testVM{
		stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
			return nil, expectedErr
		},
	}

	runtime := NewRuntime()
	result, err := runtime.Step(vm)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedErr, err)
}

func TestRuntime_Step_SchedulerError(t *testing.T) {
	vmTask := newTestTask()
	expectedErr := errors.New("scheduler error")

	vm := &testVM{
		stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
			return []*engine.Task{vmTask}, nil
		},
	}

	sh := &testScheduler{
		handleTasksFunc: func(tasks []*engine.Task) ([]*engine.Task, error) {
			return nil, expectedErr
		},
	}

	runtime := &Runtime{scheduler: sh}
	result, err := runtime.Step(vm)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), expectedErr.Error())
}

func TestRuntime_GetOpenChannels(t *testing.T) {
	expectedChannels := []string{"channel1", "channel2"}
	sh := &testScheduler{
		getOpenChannelsFunc: func() []string {
			return expectedChannels
		},
	}

	runtime := &Runtime{scheduler: sh}
	channels := runtime.GetOpenChannels()
	assert.Equal(t, expectedChannels, channels)
}

func TestRuntime_Send(t *testing.T) {
	expectedTask := newTestTask()
	channelName := "testChannel"
	value := lua.LString("test value")

	sh := &testScheduler{
		sendFunc: func(name string, val lua.LValue) ([]*engine.Task, error) {
			assert.Equal(t, channelName, name)
			assert.Equal(t, value, val)
			return []*engine.Task{expectedTask}, nil
		},
	}

	runtime := &Runtime{scheduler: sh}
	tasks, err := runtime.Send(channelName, value)
	assert.NoError(t, err)
	assert.Equal(t, []*engine.Task{expectedTask}, tasks)
}

func TestRuntime_FilterExternalTasks(t *testing.T) {
	runtime := NewRuntime()

	tests := []struct {
		name     string
		tasks    []*engine.Task
		expected int
	}{
		{
			name: "no_channel_ops",
			tasks: []*engine.Task{
				newTestTask(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{lua.LString("external_op")}
					return t
				}(),
			},
			expected: 2,
		},
		{
			name: "mixed_ops",
			tasks: []*engine.Task{
				newTestTask(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&chanOperation{}}
					return t
				}(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&selectOperation{}}
					return t
				}(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{lua.LString("external_op")}
					return t
				}(),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runtime.filterExternalTasks(tt.tasks)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestRuntime_FilterChannelTasks(t *testing.T) {
	runtime := NewRuntime()

	tests := []struct {
		name     string
		tasks    []*engine.Task
		expected int
	}{
		{
			name: "only_channel_ops",
			tasks: []*engine.Task{
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&chanOperation{}}
					return t
				}(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&selectOperation{}}
					return t
				}(),
			},
			expected: 2,
		},
		{
			name: "mixed_ops",
			tasks: []*engine.Task{
				newTestTask(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&chanOperation{}}
					return t
				}(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{&selectOperation{}}
					return t
				}(),
				func() *engine.Task {
					t := newTestTask()
					t.Yielded = []lua.LValue{lua.LString("external_op")}
					return t
				}(),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runtime.filterChannelTasks(tt.tasks)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestRuntime_Cleanup(t *testing.T) {
	closeCalled := false
	sh := &testScheduler{
		closeFunc: func() {
			closeCalled = true
		},
	}

	runtime := &Runtime{scheduler: sh}
	runtime.Cleanup()
	assert.True(t, closeCalled)
}
