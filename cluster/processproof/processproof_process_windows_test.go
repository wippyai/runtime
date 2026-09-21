// SPDX-License-Identifier: MPL-2.0

//go:build windows

package processproof

import (
	"context"
	"os"
	"os/exec"
)

func configureHelperCommand(_ *exec.Cmd) {}

func helperSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func helperDiagnosticSignal() os.Signal { return os.Interrupt }

func installHelperDiagnosticSignal(chan<- os.Signal) func() { return func() {} }

func terminateHelper(cmd *exec.Cmd) error { return cmd.Process.Kill() }
