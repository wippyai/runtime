// Package option_f implements scheduler with events passed directly to Step.
// Scheduler owns the event queue and all buffers. Process just reads events and writes yields.
// Zero-allocation design: no Completer objects, handlers call ResultReceiver directly.
package option_f

import "context"

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
	Tag   any   // correlation tag for yield completions
	Data  any   // result data or message payload
	Error error // error if yield failed
}

// Yield is a command to dispatch with correlation tag.
type Yield struct {
	Cmd Command
	Tag any
}

// StepOutput is the write-only output buffer for Process.Step().
// Scheduler owns this, process writes yields and completion status.
// Inline buffer for common case (1-2 yields), overflow slice for rare cases.
type StepOutput struct {
	buf    [2]Yield
	ext    []Yield // overflow for > 2 yields
	count  int
	done   bool
	result any
}

// Yield adds a command to be dispatched.
func (o *StepOutput) Yield(cmd Command, tag any) {
	if o.count < len(o.buf) {
		o.buf[o.count] = Yield{Cmd: cmd, Tag: tag}
	} else {
		o.ext = append(o.ext, Yield{Cmd: cmd, Tag: tag})
	}
	o.count++
}

// Done marks execution as complete with result.
func (o *StepOutput) Done(result any) {
	o.done = true
	o.result = result
}

// Reset clears output for reuse.
func (o *StepOutput) Reset() {
	o.buf[0] = Yield{}
	o.buf[1] = Yield{}
	o.ext = o.ext[:0]
	o.count = 0
	o.done = false
	o.result = nil
}

// Count returns number of yields.
func (o *StepOutput) Count() int {
	return o.count
}

// IsDone returns true if process completed.
func (o *StepOutput) IsDone() bool {
	return o.done
}

// Result returns the completion result.
func (o *StepOutput) Result() any {
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

// Command is dispatched to handlers.
type Command interface {
	CmdID() uint16
}

// ResultReceiver receives yield completion results.
// Scheduler implements this - no Completer allocation needed.
type ResultReceiver interface {
	CompleteYield(tag any, data any, err error)
}

// Handler executes commands.
// tag is the correlation tag, receiver is where to send results.
type Handler interface {
	Handle(ctx context.Context, cmd Command, tag any, receiver ResultReceiver) error
}

// Process is a schedulable state machine.
// Minimal interface - scheduler passes events, process writes to output.
type Process interface {
	// Init prepares the process.
	Init(ctx context.Context, method string, input any) error

	// Step advances state machine with events that arrived.
	// events slice is owned by scheduler, process must not retain it.
	// out is scheduler-owned buffer, process writes yields and done status.
	Step(events []Event, out *StepOutput) error

	// Close releases resources.
	Close()
}
