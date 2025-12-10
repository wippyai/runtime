package process

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
)

// StepStatus indicates the process state after Step() returns.
const (
	StepContinue StepStatus = iota
	StepIdle
	StepDone
	StepWaitYields // waiting for previously dispatched yields to complete
)

// MaxYields is the maximum yields per step that fit in the fixed buffer.
const MaxYields = 2

// StepStatus indicates the process state after Step() returns.
type StepStatus int

// EventType distinguishes yield completions from messages.
type EventType uint8

const (
	EventYieldComplete EventType = iota
	EventMessage
)

// Event is a single item delivered to Process.Step().
// Can be a yield completion or an external message.
type Event struct {
	Type  EventType
	Tag   uint64 // correlation tag for yield completions
	Data  any    // result data or message payload
	Error error  // error if yield failed
}

// Yield associates a command with a correlation tag.
// The tag is returned with the result for O(1) lookup.
type Yield struct {
	Cmd dispatcher.Command
	Tag uint64
}

// StepOutput is the write-only output buffer for Process.Step().
// Scheduler owns this, process writes yields and completion status.
// Inline buffer for common case (1-2 yields), overflow slice for rare cases.
type StepOutput struct {
	buf    [MaxYields]Yield
	ext    []Yield // overflow for > MaxYields yields
	count  int
	status StepStatus
	result payload.Payload
}

// Yield adds a command to be dispatched.
func (o *StepOutput) Yield(cmd dispatcher.Command, tag uint64) {
	if o.count < len(o.buf) {
		o.buf[o.count] = Yield{Cmd: cmd, Tag: tag}
	} else {
		o.ext = append(o.ext, Yield{Cmd: cmd, Tag: tag})
	}
	o.count++
}

// Done marks execution as complete with result.
func (o *StepOutput) Done(result payload.Payload) {
	o.status = StepDone
	o.result = result
}

// Idle marks process as waiting for external events (messages).
func (o *StepOutput) Idle() {
	o.status = StepIdle
}

// WaitYields marks process as waiting for previously dispatched yields.
func (o *StepOutput) WaitYields() {
	o.status = StepWaitYields
}

// Continue marks process as ready to continue (default).
func (o *StepOutput) Continue() {
	o.status = StepContinue
}

// Reset clears output for reuse.
func (o *StepOutput) Reset() {
	o.buf[0] = Yield{}
	o.buf[1] = Yield{}
	o.ext = o.ext[:0]
	o.count = 0
	o.status = StepContinue
	o.result = nil
}

// Count returns number of yields.
func (o *StepOutput) Count() int {
	return o.count
}

// Status returns the step status.
func (o *StepOutput) Status() StepStatus {
	return o.status
}

// IsDone returns true if process completed.
func (o *StepOutput) IsDone() bool {
	return o.status == StepDone
}

// IsIdle returns true if process is waiting for messages.
func (o *StepOutput) IsIdle() bool {
	return o.status == StepIdle
}

// Result returns the completion result.
func (o *StepOutput) Result() payload.Payload {
	return o.result
}

// Yields returns all yields as a slice.
// For iteration by scheduler only.
func (o *StepOutput) Yields() []Yield {
	if o.count == 0 {
		return nil
	}
	if o.count <= len(o.buf) {
		return o.buf[:o.count]
	}
	// Combine inline + overflow
	all := make([]Yield, o.count)
	copy(all, o.buf[:])
	copy(all[len(o.buf):], o.ext)
	return all
}

// ForEachYield iterates yields without allocation.
func (o *StepOutput) ForEachYield(fn func(y Yield)) {
	for i := 0; i < o.count && i < len(o.buf); i++ {
		fn(o.buf[i])
	}
	for _, y := range o.ext {
		fn(y)
	}
}
