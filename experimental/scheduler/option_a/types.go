package option_a

import "context"

// Event represents either a yield result or an external message
type Event interface {
	IsYieldResult() bool
}

// YieldResult is the completion of a yielded command
type YieldResult struct {
	Tag   any
	Data  any
	Error error
}

func (YieldResult) IsYieldResult() bool { return true }

// Message is an external message sent to the process
type Message struct {
	From    PID
	Payload any
}

func (Message) IsYieldResult() bool { return false }

// Yield is what a process returns when it needs async work
type Yield struct {
	Cmd Command
	Tag any
}

// StepResult is what Step() returns
type StepResult struct {
	Yields []Yield
	Done   bool
	Output any
	Error  error
}

// Command is an async operation to execute
type Command interface {
	Execute(ctx context.Context, completer Completer) error
}

// Completer is how commands report results
type Completer interface {
	Complete(data any, err error)
}

// PID is a process identifier
type PID string
