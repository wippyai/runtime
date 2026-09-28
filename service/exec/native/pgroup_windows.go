// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import (
	"os/exec"
	"syscall"

	execapi "github.com/wippyai/runtime/api/service/exec"
)

// processGroupSupported reports whether this platform can start a child in its
// own process group. Windows groups a process tree with a Job Object, which
// this executor does not yet create, so the option is refused rather than
// accepted and ignored.
const processGroupSupported = false

func applyProcessGroup(_ *exec.Cmd) {}

func signalProcessGroup(_ int, _ syscall.Signal) error {
	return execapi.ErrProcessGroupUnsupported
}
