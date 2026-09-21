// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package processproof

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func configureHelperCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func helperSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func helperDiagnosticSignal() os.Signal { return syscall.SIGQUIT }

func installHelperDiagnosticSignal(ch chan<- os.Signal) func() {
	signal.Notify(ch, syscall.SIGQUIT)
	return func() { signal.Stop(ch) }
}

func terminateHelper(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
