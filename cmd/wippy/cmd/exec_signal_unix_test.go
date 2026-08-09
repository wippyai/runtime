// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
)

// TestExecSignalSubprocessHelper is the child half of the bounded interrupt
// test below. Keeping it in a Unix-only test exercises real OS signal delivery
// while the handler represents an active process.exec child.
func TestExecSignalSubprocessHelper(t *testing.T) {
	if os.Getenv("WIPPY_EXEC_SIGNAL_HELPER") != "1" {
		return
	}

	started := make(chan struct{})
	handler := dispatcher.HandlerFunc(func(ctx context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		close(started)
		go func() {
			<-ctx.Done()
			receiver.CompleteYield(tag, process.ExecResult{}, ctx.Err())
		}()
		return nil
	})
	ctx := newExecTestContext(t, handler)

	// Keep the signal-aware context as the parent of the dispatch context while
	// retaining the test registry in the application context.
	execCtx, stopExec := newExecSignalContext(ctx)
	defer stopExec()
	fmt.Fprintln(os.Stdout, "ready")
	_, err := waitForExecResult(execCtx, &process.ExecCmd{
		Source: registry.NewID("app", "active"),
		HostID: "terminal",
	})
	require.ErrorIs(t, err, context.Canceled)
	select {
	case <-started:
	default:
		t.Fatal("exec handler did not start before signal")
	}
}

func TestExecSignalSubprocessCancelsActiveExec(t *testing.T) {
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestExecSignalSubprocessHelper")
	child.Env = append(os.Environ(), "WIPPY_EXEC_SIGNAL_HELPER=1")
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	child.Stderr = os.Stderr
	require.NoError(t, child.Start())

	waitDone := make(chan error, 1)
	go func() { waitDone <- child.Wait() }()

	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "ready" {
				close(ready)
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		<-waitDone
		t.Fatal("exec signal helper did not become ready")
	}

	require.NoError(t, child.Process.Signal(os.Interrupt))
	select {
	case err := <-waitDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		<-waitDone
		t.Fatal("active exec child did not cancel after SIGINT")
	}
}
