package exec

import (
	"context"
	"errors"
	"io"
	osexec "os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/service/exec"
)

// testCompleter wraps a callback function to implement dispatcher.Completer
type testReceiver struct {
	fn func(data any)
}

func (r *testReceiver) CompleteYield(_ uint64, data any, _ error) {
	r.fn(data)
}

type mockProcess struct {
	waitErr error
}

func (m *mockProcess) Start() error              { return nil }
func (m *mockProcess) Signal(_ int) error        { return nil }
func (m *mockProcess) WriteStdin(_ []byte) error { return nil }
func (m *mockProcess) Stdout() io.ReadCloser     { return nil }
func (m *mockProcess) Stderr() io.ReadCloser     { return nil }

func (m *mockProcess) Wait() error {
	if m.waitErr != nil {
		return m.waitErr
	}
	return nil
}

func TestProcessWaitHandler_Handle_Success(t *testing.T) {
	d := NewDispatcher()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	proc := &mockProcess{waitErr: nil}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	done := make(chan struct{})
	err := handlers[execapi.ProcessWait].Handle(context.Background(), cmd, 0, &testReceiver{fn: func(data any) {
		response = data.(execapi.ProcessWaitResponse)
		close(done)
	}})

	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	assert.Equal(t, 0, response.ExitCode)
	assert.NoError(t, response.Error)
}

func TestProcessWaitHandler_Handle_ExitError(t *testing.T) {
	d := NewDispatcher()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	exitErr := &osexec.ExitError{}
	proc := &mockProcess{waitErr: exitErr}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	done := make(chan struct{})
	err := handlers[execapi.ProcessWait].Handle(context.Background(), cmd, 0, &testReceiver{fn: func(data any) {
		response = data.(execapi.ProcessWaitResponse)
		close(done)
	}})

	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	assert.NoError(t, response.Error)
}

func TestProcessWaitHandler_Handle_OtherError(t *testing.T) {
	d := NewDispatcher()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	expectedErr := errors.New("some unexpected error")
	proc := &mockProcess{waitErr: expectedErr}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	done := make(chan struct{})
	err := handlers[execapi.ProcessWait].Handle(context.Background(), cmd, 0, &testReceiver{fn: func(data any) {
		response = data.(execapi.ProcessWaitResponse)
		close(done)
	}})

	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	assert.ErrorIs(t, response.Error, expectedErr)
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()

	var registered []dispatcher.CommandID
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered = append(registered, id)
		assert.NotNil(t, h)
	}

	d.RegisterAll(register)

	assert.Len(t, registered, 1)
	assert.Contains(t, registered, execapi.ProcessWait)
}
