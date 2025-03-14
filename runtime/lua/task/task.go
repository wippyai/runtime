package task

import (
	"errors"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// ResultCallback is a function called when a task completes
type ResultCallback func(result runtime.Result)

var (
	ErrTaskCompleted = errors.New("task already completed")
)

// Task represents a task with payload input and completion callback
type Task struct {
	// Input payload
	Input payload.Payload

	// Completion callback
	onComplete ResultCallback

	// Task state
	mu        sync.Mutex
	completed bool
	result    runtime.Result
}

// NewTask creates a new task with the given input and completion callback
func NewTask(input payload.Payload, onComplete ResultCallback) *Task {
	return &Task{
		Input:      input,
		onComplete: onComplete,
		completed:  false,
	}
}

// Complete completes the task with a successful result value
func (t *Task) Complete(value payload.Payload) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.completed {
		return ErrTaskCompleted
	}

	t.result = runtime.Result{
		Value: value,
		Error: nil,
	}
	t.completed = true

	// Call the completion handler if provided
	if t.onComplete != nil {
		t.onComplete(t.result)
	}

	return nil
}

// Fail marks the task as failed with an error
func (t *Task) Fail(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.completed {
		return ErrTaskCompleted
	}

	t.result = runtime.Result{
		Value: nil,
		Error: err,
	}
	t.completed = true

	// Call the completion handler if provided
	if t.onComplete != nil {
		t.onComplete(t.result)
	}

	return nil
}

// IsCompleted returns whether the task has completed
func (t *Task) IsCompleted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completed
}

// GetResult returns the task's result
func (t *Task) GetResult() runtime.Result {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.result
}
