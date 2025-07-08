package task

import (
	"errors"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
)

func TestNewTask(t *testing.T) {
	// Test with nil callback
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	assert.NotNil(t, task, "Task should not be nil")
	assert.Equal(t, input, task.Input, "Input should match")
	assert.Nil(t, task.onComplete, "Callback should be nil")
	assert.False(t, task.completed, "Task should not be completed initially")
	assert.Nil(t, task.result.Value, "Result value should be nil initially")
	assert.Nil(t, task.result.Error, "Result error should be nil initially")
}

func TestNewTask_WithCallback(t *testing.T) {
	// Test with callback
	input := payload.NewString("test input")
	called := false

	callback := func(_ runtime.Result) {
		called = true
	}

	task := NewTask(input, callback)

	assert.NotNil(t, task, "Task should not be nil")
	assert.Equal(t, input, task.Input, "Input should match")
	assert.NotNil(t, task.onComplete, "Callback should not be nil")
	assert.False(t, task.completed, "Task should not be completed initially")
	assert.False(t, called, "Callback should not be called initially")
}

func TestTask_Complete_Success(t *testing.T) {
	// Test successful completion
	input := payload.NewString("test input")
	resultValue := payload.NewString("test result")
	called := false
	var capturedResult runtime.Result

	callback := func(result runtime.Result) {
		called = true
		capturedResult = result
	}

	task := NewTask(input, callback)

	// Complete the task
	err := task.Complete(resultValue)

	// Assert
	assert.NoError(t, err, "Complete should not return error")
	assert.True(t, task.completed, "Task should be marked as completed")
	assert.Equal(t, resultValue, task.result.Value, "Result value should match")
	assert.Nil(t, task.result.Error, "Result error should be nil")
	assert.True(t, called, "Callback should be called")
	assert.Equal(t, resultValue, capturedResult.Value, "Captured result value should match")
	assert.Nil(t, capturedResult.Error, "Captured result error should be nil")
}

func TestTask_Complete_AlreadyCompleted(t *testing.T) {
	// Test completing an already completed task
	input := payload.NewString("test input")
	resultValue1 := payload.NewString("result 1")
	resultValue2 := payload.NewString("result 2")

	task := NewTask(input, nil)

	// Complete the task first time
	err1 := task.Complete(resultValue1)
	assert.NoError(t, err1, "First complete should not return error")

	// Try to complete again
	err2 := task.Complete(resultValue2)
	assert.Error(t, err2, "Second complete should return error")
	assert.Equal(t, ErrTaskCompleted, err2, "Should return ErrTaskCompleted")
	assert.Equal(t, resultValue1, task.result.Value, "Result should remain unchanged")
}

func TestTask_Fail_Success(t *testing.T) {
	// Test failing a task
	input := payload.NewString("test input")
	testError := errors.New("test error")
	called := false
	var capturedResult runtime.Result

	callback := func(result runtime.Result) {
		called = true
		capturedResult = result
	}

	task := NewTask(input, callback)

	// Fail the task
	err := task.Fail(testError)

	// Assert
	assert.NoError(t, err, "Fail should not return error")
	assert.True(t, task.completed, "Task should be marked as completed")
	assert.Nil(t, task.result.Value, "Result value should be nil")
	assert.Equal(t, testError, task.result.Error, "Result error should match")
	assert.True(t, called, "Callback should be called")
	assert.Nil(t, capturedResult.Value, "Captured result value should be nil")
	assert.Equal(t, testError, capturedResult.Error, "Captured result error should match")
}

func TestTask_Fail_AlreadyCompleted(t *testing.T) {
	// Test failing an already completed task
	input := payload.NewString("test input")
	resultValue := payload.NewString("test result")
	testError := errors.New("test error")

	task := NewTask(input, nil)

	// Complete the task first
	err1 := task.Complete(resultValue)
	assert.NoError(t, err1, "Complete should not return error")

	// Try to fail the task
	err2 := task.Fail(testError)
	assert.Error(t, err2, "Fail should return error")
	assert.Equal(t, ErrTaskCompleted, err2, "Should return ErrTaskCompleted")
	assert.Equal(t, resultValue, task.result.Value, "Result value should remain unchanged")
	assert.Nil(t, task.result.Error, "Result error should remain nil")
}

func TestTask_IsCompleted(t *testing.T) {
	// Test IsCompleted method
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	// Initially not completed
	assert.False(t, task.IsCompleted(), "Task should not be completed initially")

	// After completion
	err := task.Complete(payload.NewString("result"))
	assert.NoError(t, err, "Complete should not return error")
	assert.True(t, task.IsCompleted(), "Task should be completed after Complete")
}

func TestTask_GetResult(t *testing.T) {
	// Test GetResult method
	input := payload.NewString("test input")
	resultValue := payload.NewString("test result")
	task := NewTask(input, nil)

	// Initially empty result
	initialResult := task.GetResult()
	assert.Nil(t, initialResult.Value, "Initial result value should be nil")
	assert.Nil(t, initialResult.Error, "Initial result error should be nil")

	// After completion
	err := task.Complete(resultValue)
	assert.NoError(t, err, "Complete should not return error")

	finalResult := task.GetResult()
	assert.Equal(t, resultValue, finalResult.Value, "Final result value should match")
	assert.Nil(t, finalResult.Error, "Final result error should be nil")
}

func TestTask_GetResult_AfterFail(t *testing.T) {
	// Test GetResult after failure
	input := payload.NewString("test input")
	testError := errors.New("test error")
	task := NewTask(input, nil)

	// Fail the task
	err := task.Fail(testError)
	assert.NoError(t, err, "Fail should not return error")

	result := task.GetResult()
	assert.Nil(t, result.Value, "Result value should be nil after failure")
	assert.Equal(t, testError, result.Error, "Result error should match after failure")
}

func TestTask_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to task methods
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	const numGoroutines = 100
	var wg sync.WaitGroup

	// Test concurrent IsCompleted calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = task.IsCompleted()
		}()
	}
	wg.Wait()

	// Test concurrent GetResult calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = task.GetResult()
		}()
	}
	wg.Wait()

	// Complete the task
	err := task.Complete(payload.NewString("result"))
	assert.NoError(t, err, "Complete should not return error")

	// Test concurrent access after completion
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			assert.True(t, task.IsCompleted(), "Task should be completed")
		}()
	}
	wg.Wait()
}

func TestTask_CompleteAndFail_Race(t *testing.T) {
	// Test race condition between Complete and Fail
	input := payload.NewString("test input")
	resultValue := payload.NewString("test result")
	testError := errors.New("test error")

	task := NewTask(input, nil)

	// Start goroutines that try to complete and fail simultaneously
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = task.Complete(resultValue)
	}()

	go func() {
		defer wg.Done()
		_ = task.Fail(testError)
	}()

	wg.Wait()

	// One of them should succeed, the other should fail
	assert.True(t, task.completed, "Task should be completed")

	// Check that the result is consistent (either success or failure, not mixed)
	if task.result.Error != nil {
		assert.Nil(t, task.result.Value, "If error is set, value should be nil")
		assert.Equal(t, testError, task.result.Error, "Error should match")
	} else {
		assert.Equal(t, resultValue, task.result.Value, "If no error, value should match")
		assert.Nil(t, task.result.Error, "Error should be nil")
	}
}

func TestTask_CallbackCalledOnce(t *testing.T) {
	// Test that callback is called exactly once
	input := payload.NewString("test input")
	callCount := 0
	var capturedResults []runtime.Result

	callback := func(result runtime.Result) {
		callCount++
		capturedResults = append(capturedResults, result)
	}

	task := NewTask(input, callback)

	// Try to complete multiple times
	err1 := task.Complete(payload.NewString("result1"))
	err2 := task.Complete(payload.NewString("result2"))
	err3 := task.Fail(errors.New("error"))

	// Assert
	assert.NoError(t, err1, "First complete should succeed")
	assert.Error(t, err2, "Second complete should fail")
	assert.Error(t, err3, "Fail should fail")
	assert.Equal(t, 1, callCount, "Callback should be called exactly once")
	assert.Len(t, capturedResults, 1, "Should have exactly one captured result")
	assert.Equal(t, payload.NewString("result1"), capturedResults[0].Value, "Captured result should match first completion")
}

func TestTask_WithNilInput(t *testing.T) {
	// Test task with nil input
	task := NewTask(nil, nil)

	assert.NotNil(t, task, "Task should not be nil")
	assert.Nil(t, task.Input, "Input should be nil")
	assert.False(t, task.completed, "Task should not be completed initially")
}

func TestTask_CompleteWithNilValue(t *testing.T) {
	// Test completing with nil value
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	err := task.Complete(nil)

	assert.NoError(t, err, "Complete with nil should not return error")
	assert.True(t, task.completed, "Task should be marked as completed")
	assert.Nil(t, task.result.Value, "Result value should be nil")
	assert.Nil(t, task.result.Error, "Result error should be nil")
}

func TestTask_CompleteWithNilError(t *testing.T) {
	// Test failing with nil error
	input := payload.NewString("test input")
	task := NewTask(input, nil)

	err := task.Fail(nil)

	assert.NoError(t, err, "Fail with nil should not return error")
	assert.True(t, task.completed, "Task should be marked as completed")
	assert.Nil(t, task.result.Value, "Result value should be nil")
	assert.Nil(t, task.result.Error, "Result error should be nil")
}

func TestTask_StressTest(t *testing.T) {
	// Stress test with many concurrent tasks
	const numTasks = 1000
	const numGoroutines = 10

	var wg sync.WaitGroup
	tasks := make([]*Task, numTasks)

	// Create tasks
	for i := 0; i < numTasks; i++ {
		input := payload.NewString("input")
		tasks[i] = NewTask(input, nil)
	}

	// Concurrently complete tasks
	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := goroutineID; i < numTasks; i += numGoroutines {
				result := payload.NewString("result")
				_ = tasks[i].Complete(result)
			}
		}(g)
	}
	wg.Wait()

	// Verify all tasks are completed
	for i := 0; i < numTasks; i++ {
		assert.True(t, tasks[i].IsCompleted(), "All tasks should be completed")
		assert.NotNil(t, tasks[i].GetResult().Value, "All tasks should have result values")
	}
}

func TestTask_ErrorConstants(t *testing.T) {
	// Test error constants
	assert.NotNil(t, ErrTaskCompleted, "ErrTaskCompleted should not be nil")
	assert.Equal(t, "task already completed", ErrTaskCompleted.Error(), "Error message should match")
}
