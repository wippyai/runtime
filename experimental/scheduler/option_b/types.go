package option_b

// Event represents something that can wake a process.
// Could be yield completion, inbox message, timer, etc.
type Event interface {
	isEvent()
}

// YieldComplete is sent when a yield handler finishes.
type YieldComplete struct {
	Tag   any
	Data  any
	Error error
}

func (YieldComplete) isEvent() {}

// Message represents an external message arriving via Send().
type Message struct {
	Topic string
	Data  []byte
}

func (Message) isEvent() {}

// Notifier is used by Process to signal completion events.
// Scheduler owns the implementation and controls where events go.
type Notifier interface {
	Notify(event Event)
}

// Yield represents work to dispatch to handlers.
type Yield struct {
	Tag any
	Cmd string
}

// YieldResult is the result of processing a yield.
type YieldResult struct {
	Tag   any
	Data  any
	Error error
}

// StepStatus indicates what the process wants to do next.
type StepStatus int

const (
	StepContinue StepStatus = iota // Has work to do, call Step again
	StepIdle                       // Waiting for external events (inbox messages)
	StepDone                       // Execution complete
)

// StepResult contains the result of a Step() call.
type StepResult struct {
	Status StepStatus
	Yields []Yield
	Result any
}
