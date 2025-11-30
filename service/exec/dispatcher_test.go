package exec

import (
	"context"
	"errors"
	"io"
	osexec "os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/dispatcher/exec"
)

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
	handler := NewProcessWaitHandler()

	proc := &mockProcess{waitErr: nil}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	err := handler.Handle(context.Background(), cmd, func(data any) {
		response = data.(execapi.ProcessWaitResponse)
	})

	require.NoError(t, err)
	assert.Equal(t, 0, response.ExitCode)
	assert.NoError(t, response.Error)
}

func TestProcessWaitHandler_Handle_ExitError(t *testing.T) {
	handler := NewProcessWaitHandler()

	exitErr := &osexec.ExitError{}
	proc := &mockProcess{waitErr: exitErr}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	err := handler.Handle(context.Background(), cmd, func(data any) {
		response = data.(execapi.ProcessWaitResponse)
	})

	require.NoError(t, err)
	assert.NoError(t, response.Error)
}

func TestProcessWaitHandler_Handle_OtherError(t *testing.T) {
	handler := NewProcessWaitHandler()

	expectedErr := errors.New("some unexpected error")
	proc := &mockProcess{waitErr: expectedErr}

	cmd := &execapi.ProcessWaitCmd{
		Process: proc,
	}

	var response execapi.ProcessWaitResponse
	err := handler.Handle(context.Background(), cmd, func(data any) {
		response = data.(execapi.ProcessWaitResponse)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, response.Error, expectedErr)
}

func TestNewDispatcherService(t *testing.T) {
	svc := NewDispatcherService()
	assert.NotNil(t, svc.Wait)
}

func TestDispatcherService_RegisterAll(t *testing.T) {
	svc := NewDispatcherService()

	var registered []dispatcher.CommandID
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered = append(registered, id)
		assert.NotNil(t, h)
	}

	svc.RegisterAll(register)

	assert.Len(t, registered, 1)
	assert.Contains(t, registered, execapi.CmdProcessWait)
}
