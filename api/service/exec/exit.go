// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"errors"
	osexec "os/exec"
	"syscall"
)

// ExitStatus describes how a child process finished.
//
// Code is the exit code the child reported. A child killed by a signal has no
// exit code of its own, so it is reported as 128+Signal, the encoding shells
// use and the one the native executor produces.
//
// Signal carries the signal that killed the child, or 0 when the child exited
// on its own or the executor reports only a code, as a container exit does.
//
// Err is set only when the exit could not be observed at all: a wait that could
// not be performed, a transport failure to a remote executor. A non-zero exit
// is not an error, it is Code.
type ExitStatus struct {
	Err    error
	Code   int
	Signal int
}

// ExitCoder is an error carrying the exit code of the process it describes.
type ExitCoder interface {
	ExitCode() int
}

// ExitReporter is a Process that owns the reap of its child and can therefore
// report the outcome more than once. Wait can only be performed once, so a
// caller that has an ExitReporter must use it instead: several parts of a
// supervisor may need the exit of the same child.
type ExitReporter interface {
	AwaitExit() ExitStatus
}

// ClassifyExit turns the error Process.Wait returns into an exit status.
func ClassifyExit(err error) ExitStatus {
	if err == nil {
		return ExitStatus{}
	}

	var coder ExitCoder
	if !errors.As(err, &coder) {
		return ExitStatus{Err: err}
	}

	status := ExitStatus{Code: coder.ExitCode(), Signal: exitSignal(err)}
	if status.Signal != 0 && status.Code < 0 {
		status.Code = 128 + status.Signal
	}
	return status
}

// WaitFor reports how a process finished, reaping it when it does not own its
// own reap.
func WaitFor(p Process) ExitStatus {
	if reporter, ok := p.(ExitReporter); ok {
		return reporter.AwaitExit()
	}
	return ClassifyExit(p.Wait())
}

// exitSignal reads the signal that killed the child from the platform wait
// status. Executors that report only an exit code carry no wait status and
// report no signal.
func exitSignal(err error) int {
	var exitErr *osexec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil {
		return 0
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return 0
	}
	return int(status.Signal())
}
