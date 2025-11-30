package exec

import (
	"context"
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"testing"

	execapi "github.com/wippyai/runtime/api/dispatcher/exec"
)

// mockProcess implements the Process interface for testing
type mockProcess struct {
	waitErr   error
	waitCalls int
}

func (m *mockProcess) Start() error                 { return nil }
func (m *mockProcess) Signal(sig int) error         { return nil }
func (m *mockProcess) WriteStdin(data []byte) error { return nil }
func (m *mockProcess) Stdout() io.ReadCloser        { return nil }
func (m *mockProcess) Stderr() io.ReadCloser        { return nil }
func (m *mockProcess) Wait() error {
	m.waitCalls++
	return m.waitErr
}

func TestProcessWaitHandler(t *testing.T) {
	h := NewProcessWaitHandler()
	proc := &mockProcess{}
	var resp execapi.ProcessWaitResponse

	err := h.Handle(context.Background(), &execapi.ProcessWaitCmd{
		Process: proc,
	}, func(data any) {
		resp = data.(execapi.ProcessWaitResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
	if proc.waitCalls != 1 {
		t.Errorf("expected 1 wait call, got %d", proc.waitCalls)
	}
}

func TestProcessWaitHandlerExitCode(t *testing.T) {
	// Use real process to test exit code extraction
	cmd := osexec.Command("sh", "-c", "exit 42")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start process: %v", err)
	}

	h := NewProcessWaitHandler()
	var resp execapi.ProcessWaitResponse

	err := h.Handle(context.Background(), &execapi.ProcessWaitCmd{
		Process: &realProcess{cmd: cmd},
	}, func(data any) {
		resp = data.(execapi.ProcessWaitResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if resp.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", resp.ExitCode)
	}
}

func TestProcessWaitHandlerError(t *testing.T) {
	h := NewProcessWaitHandler()
	expectedErr := errors.New("process failed")
	proc := &mockProcess{waitErr: expectedErr}
	var resp execapi.ProcessWaitResponse

	err := h.Handle(context.Background(), &execapi.ProcessWaitCmd{
		Process: proc,
	}, func(data any) {
		resp = data.(execapi.ProcessWaitResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, resp.Error)
	}
}

// realProcess wraps os/exec.Cmd to implement the Process interface
type realProcess struct {
	cmd *osexec.Cmd
}

func (p *realProcess) Start() error { return p.cmd.Start() }
func (p *realProcess) Signal(sig int) error {
	return p.cmd.Process.Signal(os.Signal(syscallSignal(sig)))
}
func (p *realProcess) WriteStdin(data []byte) error { return nil }
func (p *realProcess) Stdout() io.ReadCloser        { return nil }
func (p *realProcess) Stderr() io.ReadCloser        { return nil }
func (p *realProcess) Wait() error                  { return p.cmd.Wait() }

func syscallSignal(sig int) os.Signal {
	return os.Signal(nil)
}

func TestExecService(t *testing.T) {
	svc := NewService()
	if svc.Wait == nil {
		t.Error("Wait handler not initialized")
	}
}
