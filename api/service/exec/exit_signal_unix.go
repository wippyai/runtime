// SPDX-License-Identifier: MPL-2.0

//go:build unix

package exec

import (
	"errors"
	osexec "os/exec"
	"syscall"
)

// exitSignal reads the signal that killed the child from a Unix wait status.
// Executors that report only an exit code carry no wait status and report no
// signal.
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
