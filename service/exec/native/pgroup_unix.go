// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package native

import (
	"os/exec"
	"syscall"
)

// processGroupSupported reports whether this platform can start a child in its
// own process group.
const processGroupSupported = true

// applyProcessGroup makes the child the leader of a new process group. Every
// process it spawns inherits that group unless it deliberately leaves it, so a
// signal addressed to the group reaches the whole tree.
func applyProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup delivers sig to every member of the group led by pgid.
func signalProcessGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}
