// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"errors"
	"io"
	osexec "os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

type codedExit struct{ code int }

func (e *codedExit) Error() string { return "exited" }
func (e *codedExit) ExitCode() int { return e.code }

type signaledExit struct {
	code   int
	signal int
}

func (e *signaledExit) Error() string   { return "signaled" }
func (e *signaledExit) ExitCode() int   { return e.code }
func (e *signaledExit) ExitSignal() int { return e.signal }

type reportedExitError struct{ status ExitStatus }

func (e *reportedExitError) Error() string          { return "exit finalization failed" }
func (e *reportedExitError) ExitStatus() ExitStatus { return e.status }

func TestClassifyExitReportsCleanExit(t *testing.T) {
	require.Equal(t, ExitStatus{}, ClassifyExit(nil))
}

func TestClassifyExitReportsCodeFromExecutorError(t *testing.T) {
	status := ClassifyExit(&codedExit{code: 137})

	require.Equal(t, 137, status.Code)
	require.Zero(t, status.Signal)
	require.NoError(t, status.Err)
}

func TestClassifyExitReportsExecutorSignal(t *testing.T) {
	status := ClassifyExit(&signaledExit{code: 143, signal: 15})

	require.Equal(t, 143, status.Code)
	require.Equal(t, 15, status.Signal)
	require.NoError(t, status.Err)
}

func TestClassifyExitKeepsAnErrorThatIsNotAnExit(t *testing.T) {
	wait := errors.New("transport closed")

	status := ClassifyExit(wait)

	require.ErrorIs(t, status.Err, wait)
	require.Zero(t, status.Code)
}

func TestClassifyExitPreservesObservedExitAndOperationalFailure(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	status := ClassifyExit(&reportedExitError{status: ExitStatus{
		Code: 137,
		Err:  cleanupErr,
	}})

	require.Equal(t, 137, status.Code)
	require.ErrorIs(t, status.Err, cleanupErr)
}

func TestClassifyExitPreservesFailuresOutsideStatusError(t *testing.T) {
	cleanupA := errors.New("cleanup A failed")
	cleanupB := errors.New("cleanup B failed")
	err := errors.Join(
		&reportedExitError{status: ExitStatus{Code: 137, Err: cleanupA}},
		cleanupB,
	)

	status := ClassifyExit(err)
	require.Equal(t, 137, status.Code)
	require.ErrorIs(t, status.Err, cleanupA)
	require.ErrorIs(t, status.Err, cleanupB)
}

func TestClassifyExitReportsOrdinaryExitCode(t *testing.T) {
	if _, err := osexec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := osexec.CommandContext(t.Context(), "sh", "-c", "exit 5")

	status := ClassifyExit(cmd.Run())

	require.Equal(t, 5, status.Code)
	require.Zero(t, status.Signal)
	require.NoError(t, status.Err)
}

type reportingProcess struct {
	status    ExitStatus
	waitCalls int
}

func (p *reportingProcess) Start() error            { return nil }
func (p *reportingProcess) Signal(int) error        { return nil }
func (p *reportingProcess) WriteStdin([]byte) error { return nil }
func (p *reportingProcess) Stdout() io.ReadCloser   { return nil }
func (p *reportingProcess) Stderr() io.ReadCloser   { return nil }
func (p *reportingProcess) AwaitExit() ExitStatus   { return p.status }
func (p *reportingProcess) Wait() error             { p.waitCalls++; return nil }

type waitingProcess struct{ waitErr error }

func (p *waitingProcess) Start() error            { return nil }
func (p *waitingProcess) Signal(int) error        { return nil }
func (p *waitingProcess) WriteStdin([]byte) error { return nil }
func (p *waitingProcess) Stdout() io.ReadCloser   { return nil }
func (p *waitingProcess) Stderr() io.ReadCloser   { return nil }
func (p *waitingProcess) Wait() error             { return p.waitErr }

// A process that owns its own reap can report the exit repeatedly; waiting on
// it a second time is what fails.
func TestWaitForPrefersTheProcessOwnReap(t *testing.T) {
	process := &reportingProcess{status: ExitStatus{Code: 143, Signal: 15}}

	require.Equal(t, process.status, WaitFor(process))
	require.Zero(t, process.waitCalls)
}

func TestWaitForPreservesObservedExitAndAllOperationalFailures(t *testing.T) {
	cleanupA := errors.New("cleanup A failed")
	cleanupB := errors.New("cleanup B failed")
	process := &waitingProcess{waitErr: errors.Join(
		&reportedExitError{status: ExitStatus{Code: 137, Err: cleanupA}},
		cleanupB,
	)}

	status := WaitFor(process)
	require.Equal(t, 137, status.Code)
	require.ErrorIs(t, status.Err, cleanupA)
	require.ErrorIs(t, status.Err, cleanupB)
}
