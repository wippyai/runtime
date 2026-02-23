// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
)

type testCommand struct {
	id dispatcher.CommandID
}

func (c testCommand) CmdID() dispatcher.CommandID { return c.id }

func TestChildExitEventFields(t *testing.T) {
	exitEvent := childExitEvent{
		ChildPID: pid.PID{
			Node:   "node1",
			Host:   "host1",
			UniqID: "child-123",
		},
		Result: payload.NewString("result"),
		Error:  nil,
	}

	assert.Equal(t, "node1", exitEvent.ChildPID.Node)
	assert.Equal(t, "host1", exitEvent.ChildPID.Host)
	assert.Equal(t, "child-123", exitEvent.ChildPID.UniqID)
	assert.NotNil(t, exitEvent.Result)
	assert.Nil(t, exitEvent.Error)
}

func TestChildExitEventWithError(t *testing.T) {
	testErr := assert.AnError
	exitEvent := childExitEvent{
		ChildPID: pid.PID{UniqID: "child-456"},
		Result:   nil,
		Error:    testErr,
	}

	assert.Equal(t, "child-456", exitEvent.ChildPID.UniqID)
	assert.Nil(t, exitEvent.Result)
	assert.Equal(t, testErr, exitEvent.Error)
}

func TestIncomingSignalFields(t *testing.T) {
	payloads := payload.Payloads{
		payload.NewString("arg1"),
		payload.NewString("arg2"),
	}
	sig := incomingSignal{
		Name:     "my-signal",
		Payloads: payloads,
	}

	assert.Equal(t, "my-signal", sig.Name)
	assert.Len(t, sig.Payloads, 2)
}

func TestIncomingSignalEmpty(t *testing.T) {
	sig := incomingSignal{
		Name:     "empty-signal",
		Payloads: nil,
	}

	assert.Equal(t, "empty-signal", sig.Name)
	assert.Nil(t, sig.Payloads)
}

func TestSignalQueueOperations(t *testing.T) {
	var queue []incomingSignal

	queue = append(queue, incomingSignal{Name: "sig1"})
	queue = append(queue, incomingSignal{Name: "sig2"})
	queue = append(queue, incomingSignal{Name: "sig3"})

	assert.Len(t, queue, 3)

	// Process first signal
	first := queue[0]
	assert.Equal(t, "sig1", first.Name)

	// Clear processed signals
	queue = queue[:0]
	assert.Empty(t, queue)
	assert.Equal(t, 0, len(queue))
}

func TestChildExitQueueOperations(t *testing.T) {
	var exits []childExitEvent

	exits = append(exits, childExitEvent{ChildPID: pid.PID{UniqID: "1"}})
	exits = append(exits, childExitEvent{ChildPID: pid.PID{UniqID: "2"}})

	assert.Len(t, exits, 2)

	// Clear queue
	exits = exits[:0]
	assert.Empty(t, exits)
}

func TestMaxSignalQueueSize(t *testing.T) {
	assert.Equal(t, 10000, maxSignalQueueSize)
}

func TestMaxChildExitQueueSize(t *testing.T) {
	assert.Equal(t, 1000, maxChildExitQueueSize)
}

func TestSignalQueueLimit(t *testing.T) {
	signals := make([]incomingSignal, 0, maxSignalQueueSize)

	for i := 0; i < maxSignalQueueSize; i++ {
		signals = append(signals, incomingSignal{Name: "sig"})
	}

	assert.Len(t, signals, maxSignalQueueSize)

	// Verify queue is at limit
	assert.True(t, len(signals) >= maxSignalQueueSize)
}

func TestChildExitQueueLimit(t *testing.T) {
	exits := make([]childExitEvent, 0, maxChildExitQueueSize)

	for i := 0; i < maxChildExitQueueSize; i++ {
		exits = append(exits, childExitEvent{})
	}

	assert.Len(t, exits, maxChildExitQueueSize)

	// Verify queue is at limit
	assert.True(t, len(exits) >= maxChildExitQueueSize)
}

func TestCopyOutputYieldsSurvivesReset(t *testing.T) {
	var out process.StepOutput

	out.Yield(testCommand{id: 1}, 11)
	out.Yield(testCommand{id: 2}, 22)
	out.Yield(testCommand{id: 3}, 33)

	snapshot := copyOutputYields(&out)
	out.Reset()

	assert.Len(t, snapshot, 3)
	assert.Equal(t, uint64(11), snapshot[0].Tag)
	assert.Equal(t, uint64(22), snapshot[1].Tag)
	assert.Equal(t, uint64(33), snapshot[2].Tag)
	assert.NotNil(t, snapshot[0].Cmd)
	assert.NotNil(t, snapshot[1].Cmd)
	assert.NotNil(t, snapshot[2].Cmd)
}
