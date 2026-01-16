package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
)

type mockCommand struct {
	id dispatcher.CommandID
}

func (m *mockCommand) CmdID() dispatcher.CommandID {
	return m.id
}

func TestProcess_SetPending(t *testing.T) {
	p := &Process{}
	cmd := &mockCommand{id: 1}

	p.SetPending(cmd)

	assert.Equal(t, cmd, p.pendingCmd)
}

func TestProcess_GetResult(t *testing.T) {
	p := &Process{
		result:    42,
		resultErr: nil,
	}

	val, err := p.GetResult()

	assert.Equal(t, uint64(42), val)
	assert.NoError(t, err)
}

func TestProcess_GetResult_WithError(t *testing.T) {
	p := &Process{
		result:    0,
		resultErr: assert.AnError,
	}

	val, err := p.GetResult()

	assert.Equal(t, uint64(0), val)
	assert.Equal(t, assert.AnError, err)
}

func TestProcess_ClearPending(t *testing.T) {
	p := &Process{
		pendingCmd: &mockCommand{id: 1},
		result:     100,
		resultErr:  assert.AnError,
	}

	p.ClearPending()

	assert.Nil(t, p.pendingCmd)
	assert.Equal(t, uint64(0), p.result)
	assert.Nil(t, p.resultErr)
}

func TestProcess_Reset(t *testing.T) {
	p := &Process{
		started:    true,
		fn:         nil,
		fnArgs:     []uint64{1, 2, 3},
		tag:        5,
		pendingCmd: &mockCommand{},
		result:     100,
		resultErr:  assert.AnError,
	}

	p.reset()

	assert.False(t, p.started)
	assert.Nil(t, p.fn)
	assert.Empty(t, p.fnArgs)
	assert.Equal(t, uint64(0), p.tag)
	assert.Nil(t, p.pendingCmd)
	assert.Equal(t, uint64(0), p.result)
	assert.Nil(t, p.resultErr)
}

func TestProcess_StepSync_NotStarted(t *testing.T) {
	p := &Process{
		started:  false,
		asyncify: nil,
	}

	out := &process.StepOutput{}
	err := p.Step(nil, out)

	require.Error(t, err)
}

func TestProcess_StepSync_AlreadyDone(t *testing.T) {
	p := &Process{
		started:  true,
		asyncify: nil,
	}

	out := &process.StepOutput{}
	err := p.Step(nil, out)

	assert.NoError(t, err)
	assert.Equal(t, process.StepDone, out.Status())
}

func TestProcess_Step_ProcessesYieldComplete(t *testing.T) {
	p := &Process{
		started:  true,
		asyncify: nil,
	}

	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  1,
			Data: uint64(42),
		},
	}

	out := &process.StepOutput{}
	_ = p.Step(events, out)

	assert.Equal(t, uint64(42), p.result)
	assert.Nil(t, p.resultErr)
}

func TestProcess_Step_ProcessesYieldCompleteWithError(t *testing.T) {
	p := &Process{
		started:  true,
		asyncify: nil,
	}

	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   1,
			Error: assert.AnError,
		},
	}

	out := &process.StepOutput{}
	_ = p.Step(events, out)

	assert.Equal(t, assert.AnError, p.resultErr)
}

func TestProcess_Step_ProcessesInt64Data(t *testing.T) {
	p := &Process{
		started:  true,
		asyncify: nil,
	}

	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  1,
			Data: int64(100),
		},
	}

	out := &process.StepOutput{}
	_ = p.Step(events, out)

	assert.Equal(t, uint64(100), p.result)
}

func TestNewProcess(t *testing.T) {
	p := NewProcess(nil, nil)

	assert.NotNil(t, p)
	assert.Nil(t, p.runtime)
	assert.Nil(t, p.module)
	assert.Nil(t, p.transport)
}

func TestNewProcessWithTransport(t *testing.T) {
	p := NewProcessWithTransport(nil, nil, nil)

	assert.NotNil(t, p)
	assert.Nil(t, p.transport)
}

func TestProcess_Close_NilInstance(t *testing.T) {
	p := &Process{
		instance: nil,
		ctx:      context.Background(),
	}

	p.Close()

	assert.Nil(t, p.ctx)
}
