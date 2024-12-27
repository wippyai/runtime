package helpers

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// SimpleExecutor is an Executor that always returns "Hello, world!" as a string payload.
type SimpleExecutor struct{}

// Execute implements the Executor interface.
func (e *SimpleExecutor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	resultChan := make(chan *runtime.Result, 1) // Buffered channel to prevent blocking
	resultChan <- &runtime.Result{
		Payload: payload.New(struct {
			Message string `json:"message"`
		}{
			Message: "Hello, world!",
		}),
		Error: nil,
	}
	close(resultChan)

	return resultChan, nil
}

// NewSimpleExecutor creates a new instance of SimpleExecutor.
func NewSimpleExecutor() runtime.Executor {
	return &SimpleExecutor{}
}
