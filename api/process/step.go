// SPDX-License-Identifier: MPL-2.0

package process

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// StepStatus indicates the process state after Step() returns.
const (
	StepContinue StepStatus = iota
	StepIdle
	StepDone
	StepYield   // has yields to dispatch, wait for completions
	StepUpgrade // process requested upgrade, worker handles swap
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
	Data  any
	Error error
	Tag   uint64
	Type  EventType
}

// Yield associates a command with a correlation tag.
// The tag is returned with the result for O(1) lookup.
type Yield struct {
	Cmd dispatcher.Command
	Tag uint64
}

// UpgradeRequest contains information for process upgrade.
// Worker uses this to create new process and swap.
type UpgradeRequest struct {
	Source registry.ID      // target definition (empty = same definition)
	Input  payload.Payloads // args for new process
}

// StepOutput is the write-only output buffer for Process.Step().
// Scheduler owns this, process writes yields and completion status.
// Inline buffer for common case (1-2 yields), overflow slice for rare cases.
type StepOutput struct {
	result  payload.Payload
	upgrade *UpgradeRequest
	buf     [MaxYields]Yield
	ext     []Yield
	count   int
	status  StepStatus
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

// WaitForYields marks process as having yields to dispatch and waiting for completions.
func (o *StepOutput) WaitForYields() {
	o.status = StepYield
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
	o.upgrade = nil
}

// SetUpgrade sets upgrade request and status.
func (o *StepOutput) SetUpgrade(req *UpgradeRequest) {
	o.upgrade = req
	o.status = StepUpgrade
}

// Upgrade returns the upgrade request, or nil if not upgrading.
func (o *StepOutput) Upgrade() *UpgradeRequest {
	return o.upgrade
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
