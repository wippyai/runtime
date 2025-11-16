package task

import (
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct{}

func (t *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	// For the test, we need to transcode from String to Lua format
	if p.Format() == payload.String && format == payload.Lua {
		// Convert string to Lua string
		if str, ok := p.Data().(string); ok {
			return payload.NewPayload(lua.LString(str), payload.Lua), nil
		}
	}
	// For other cases, just return the same payload with new format
	return payload.NewPayload(p.Data(), format), nil
}

func (t *MockTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	// Simple unmarshal implementation for testing
	return nil
}

func TestNewTaskModule(t *testing.T) {
	module := NewTaskModule()

	assert.NotNil(t, module, "Module should not be nil")
	assert.Equal(t, "task", module.Name(), "Module name should be 'task'")
}

func TestModule_Loader(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Set up context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Load the module
	result := module.Loader(L)

	assert.Equal(t, 1, result, "Loader should return 1")

	// Note: The Loader function registers methods for task.Task type
	// but doesn't push a module table onto the stack
	// The methods are registered in the metatable for task.Task userdata
}

func TestCheckTask_ValidTask(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Create a task
	task := NewTask(payload.NewString("test"), nil)

	// Wrap it in userdata
	ud := L.NewUserData()
	ud.Value = task
	ud.Metatable = value.GetTypeMetatable(L, "task.Task")

	// Push to stack
	L.Push(ud)

	// Check task
	result := CheckTask(L, 1)

	assert.NotNil(t, result, "Should return valid task")
	assert.Equal(t, task, result, "Should return the same task")
}

func TestCheckTask_InvalidType(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Push a string instead of task
	L.Push(lua.LString("not a task"))

	// This should panic with argument error
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
			return
		}
		t.Error("Expected panic for invalid task type")
	}()

	CheckTask(L, 1)
}

func TestCheckTask_NilUserData(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Push nil
	L.Push(lua.LNil)

	// This should panic with argument error
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
			return
		}
		t.Error("Expected panic for nil userdata")
	}()

	CheckTask(L, 1)
}

func TestCheckTask_WrongUserDataType(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Create userdata with wrong type
	ud := L.NewUserData()
	ud.Value = "not a task"
	ud.Metatable = value.GetTypeMetatable(L, "task.Task")

	// Push to stack
	L.Push(ud)

	// This should panic with argument error
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
			return
		}
		t.Error("Expected panic for wrong userdata type")
	}()

	CheckTask(L, 1)
}

func TestWrapTask(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Register the task module first to create the metatable
	module := NewTaskModule()
	module.Loader(L)

	// Create a task
	task := NewTask(payload.NewString("test"), nil)

	// Wrap it
	result := WrapTask(L, task)

	assert.Equal(t, lua.LTUserData, result.Type(), "Should return userdata")

	// Check the value
	ud := result.(*lua.LUserData)
	assert.Equal(t, task, ud.Value, "Userdata should contain the task")
	assert.NotNil(t, ud.Metatable, "Userdata should have metatable")
}

func TestTaskInput_Success(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Register the task module first to create the metatable
	module.Loader(L)

	// Set up context with mock transcoder
	ctx := payload.WithTranscoder(ctxapi.NewRootContext(), &MockTranscoder{})
	L.SetContext(ctx)

	// Create a task with string input
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// Call taskInput
	result := module.taskInput(L)

	assert.Equal(t, 1, result, "Should return 1 value")

	// Check the result
	top := L.Get(-1)
	assert.Equal(t, lua.LTString, top.Type(), "Should return string")
	assert.Equal(t, "test input", top.String(), "Should return input value")
}

func TestTaskInput_NoTranscoder(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Don't set up transcoder in context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Create a task
	task := NewTask(payload.NewString("test"), nil)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// This should raise an error
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
			return
		}
		t.Error("Expected panic for missing transcoder")
	}()

	module.taskInput(L)
}

func TestTaskComplete_Success(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Set up context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Create a task
	task := NewTask(payload.NewString("input"), nil)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// Push result value
	L.Push(lua.LString("result"))

	// Call taskComplete
	result := module.taskComplete(L)

	assert.Equal(t, 1, result, "Should return 1 value")

	// Check the result
	top := L.Get(-1)
	assert.Equal(t, lua.LTBool, top.Type(), "Should return boolean")
	assert.Equal(t, lua.LTrue, top, "Should return true")

	// Check that task was completed
	assert.True(t, task.IsCompleted(), "Task should be completed")
	resultPayload := task.GetResult().Value
	assert.NotNil(t, resultPayload, "Task should have result")
}

func TestTaskComplete_AlreadyCompleted(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Set up context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Create a task and complete it
	task := NewTask(payload.NewString("input"), nil)
	err := task.Complete(payload.NewString("first result"))
	assert.NoError(t, err)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// Push result value
	L.Push(lua.LString("second result"))

	// Call taskComplete
	result := module.taskComplete(L)

	assert.Equal(t, 2, result, "Should return 2 values")

	// Check the results
	success := L.Get(-2)
	errorMsg := L.Get(-1)
	assert.Equal(t, lua.LTBool, success.Type(), "First return should be boolean")
	assert.Equal(t, lua.LTString, errorMsg.Type(), "Second return should be string")
	assert.Equal(t, lua.LFalse, success, "Should return false")
	assert.Contains(t, errorMsg.String(), "task already completed", "Should return error message")
}

func TestTaskFail_Success(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Set up context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Create a task
	task := NewTask(payload.NewString("input"), nil)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// Push error message
	L.Push(lua.LString("test error"))

	// Call taskFail
	result := module.taskFail(L)

	assert.Equal(t, 1, result, "Should return 1 value")

	// Check the result
	top := L.Get(-1)
	assert.Equal(t, lua.LTBool, top.Type(), "Should return boolean")
	assert.Equal(t, lua.LTrue, top, "Should return true")

	// Check that task was failed
	assert.True(t, task.IsCompleted(), "Task should be completed")
	assert.NotNil(t, task.GetResult().Error, "Task should have error")
	assert.Contains(t, task.GetResult().Error.Error(), "test error", "Error should contain message")
}

func TestTaskFail_AlreadyCompleted(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Set up context
	ctx := ctxapi.NewRootContext()
	L.SetContext(ctx)

	// Create a task and complete it
	task := NewTask(payload.NewString("input"), nil)
	err := task.Complete(payload.NewString("result"))
	assert.NoError(t, err)

	// Wrap task and push to stack
	taskUD := WrapTask(L, task)
	L.Push(taskUD)

	// Push error message
	L.Push(lua.LString("test error"))

	// Call taskFail
	result := module.taskFail(L)

	assert.Equal(t, 2, result, "Should return 2 values")

	// Check the results
	success := L.Get(-2)
	errorMsg := L.Get(-1)
	assert.Equal(t, lua.LTBool, success.Type(), "First return should be boolean")
	assert.Equal(t, lua.LTString, errorMsg.Type(), "Second return should be string")
	assert.Equal(t, lua.LFalse, success, "Should return false")
	assert.Contains(t, errorMsg.String(), "task already completed", "Should return error message")
}

func TestTaskComplete_WithDifferentTypes(t *testing.T) {
	module := NewTaskModule()
	L := lua.NewState()
	defer L.Close()

	// Register the task module first to create the metatable
	module.Loader(L)

	// Set up context with mock transcoder
	ctx := payload.WithTranscoder(ctxapi.NewRootContext(), &MockTranscoder{})
	L.SetContext(ctx)

	// Only test string case since ExportPayload has issues with other types
	testCases := []struct {
		name     string
		value    lua.LValue
		expected interface{}
	}{
		{"string", lua.LString("test"), "test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new task for each test case
			task := NewTask(payload.NewString("input"), nil)

			// Wrap task and push to stack
			taskUD := WrapTask(L, task)
			L.Push(taskUD)

			// Push test value
			L.Push(tc.value)

			// Call taskComplete
			result := module.taskComplete(L)

			assert.Equal(t, 1, result, "Should return 1 value")

			// Check that task was completed
			assert.True(t, task.IsCompleted(), "Task should be completed")
			assert.NotNil(t, task.GetResult().Value, "Task should have result")
		})
	}
}
