// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	terminalservice "github.com/wippyai/runtime/service/terminal"
)

type execTestRegistry struct {
	handler dispatcher.Handler
}

func (r *execTestRegistry) Get(id dispatcher.CommandID) dispatcher.Handler {
	if id == process.Exec {
		return r.handler
	}
	return nil
}

func (r *execTestRegistry) Has(id dispatcher.CommandID) bool {
	return id == process.Exec && r.handler != nil
}

func (r *execTestRegistry) Register(id dispatcher.CommandID, handler dispatcher.Handler) {
	if id == process.Exec {
		r.handler = handler
	}
}

func (r *execTestRegistry) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return r.Get(cmd.CmdID())
}

func newExecTestContext(t *testing.T, handler dispatcher.Handler) context.Context {
	t.Helper()
	app := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), app)
	require.NoError(t, dispatcher.WithRegistry(ctx, &execTestRegistry{handler: handler}))
	return ctx
}

func TestWaitForExecResult_WaitsForCompletion(t *testing.T) {
	started := make(chan struct{})
	handler := dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		close(started)
		time.AfterFunc(25*time.Millisecond, func() {
			receiver.CompleteYield(tag, process.ExecResult{
				Result: &runtime.Result{Value: payload.New(int64(0))},
			}, nil)
		})
		return nil
	})

	ctx := newExecTestContext(t, handler)
	command := &process.ExecCmd{Source: registry.NewID("app", "runner"), HostID: "terminal"}
	startedAt := time.Now()
	result, err := waitForExecResult(ctx, command)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.GreaterOrEqual(t, time.Since(startedAt), 20*time.Millisecond)
	assert.Equal(t, 0, terminalservice.ExitCode(result))
	select {
	case <-started:
	default:
		t.Fatal("exec handler was not called")
	}
}

func TestWaitForExecResult_PreservesRunnerFailureResult(t *testing.T) {
	handler := dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		receiver.CompleteYield(tag, process.ExecResult{
			Result: &runtime.Result{Value: payload.New(int64(1))},
		}, nil)
		return nil
	})

	result, err := waitForExecResult(
		newExecTestContext(t, handler),
		&process.ExecCmd{Source: registry.NewID("app", "runner"), HostID: "terminal"},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, terminalservice.ExitCode(result))
}

func TestWaitForExecResult_PreservesRuntimeError(t *testing.T) {
	handler := dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		receiver.CompleteYield(tag, process.ExecResult{
			Result: &runtime.Result{Error: errors.New("runner failed")},
		}, nil)
		return nil
	})

	result, err := waitForExecResult(
		newExecTestContext(t, handler),
		&process.ExecCmd{Source: registry.NewID("app", "runner"), HostID: "terminal"},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, terminalservice.ExitCode(result))
}

func TestWaitForExecResult_PropagatesHandlerError(t *testing.T) {
	handler := dispatcher.HandlerFunc(func(context.Context, dispatcher.Command, uint64, dispatcher.ResultReceiver) error {
		return errors.New("dispatch failed")
	})

	result, err := waitForExecResult(
		newExecTestContext(t, handler),
		&process.ExecCmd{Source: registry.NewID("app", "runner"), HostID: "terminal"},
	)
	assert.Nil(t, result)
	assert.EqualError(t, err, "dispatch failed")
}

func TestWaitForExecResult_ContextCancellationIsBounded(t *testing.T) {
	handler := dispatcher.HandlerFunc(func(context.Context, dispatcher.Command, uint64, dispatcher.ResultReceiver) error {
		return nil
	})

	ctx, cancel := context.WithCancel(newExecTestContext(t, handler))
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result, err := waitForExecResult(ctx, &process.ExecCmd{
		Source: registry.NewID("app", "runner"),
		HostID: "terminal",
	})
	assert.Nil(t, result)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExecSignalContext_CancelsOnExternalInterrupt(t *testing.T) {
	tests := []struct {
		sig  os.Signal
		name string
	}{
		{name: "interrupt", sig: os.Interrupt},
		{name: "terminate", sig: syscall.SIGTERM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetExecShutdownState(t)
			ctx, stop := newExecSignalContext(context.Background())
			defer stop()

			current, err := os.FindProcess(os.Getpid())
			require.NoError(t, err)
			require.NoError(t, current.Signal(tt.sig))

			select {
			case <-ctx.Done():
				assert.ErrorIs(t, ctx.Err(), context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatalf("%s did not cancel exec context", tt.sig)
			}
		})
	}
}

func TestExecSignalContext_DoesNotReactToSupervisorChannel(t *testing.T) {
	resetExecShutdownState(t)
	t.Cleanup(func() { resetExecShutdownState(t) })
	ctx, stop := newExecSignalContext(context.Background())
	defer stop()

	app := ctxapi.NewAppContext()
	supervisorCtx := ctxapi.WithAppContext(context.Background(), app)
	supervisorapi.SetSignalChannel(supervisorCtx, make(chan os.Signal, 1))
	supervisorapi.TriggerShutdown(supervisorCtx, 0)

	select {
	case <-ctx.Done():
		t.Fatal("internal supervisor channel signal canceled exec context")
	case <-time.After(100 * time.Millisecond):
	}
}

func resetExecShutdownState(t *testing.T) {
	t.Helper()
	app := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), app)
	supervisorapi.SetSignalChannel(ctx, make(chan os.Signal, 1))
}

// TestExecSignalSubprocessHelper is the child half of the bounded interrupt
// test below. Keeping it in the test binary exercises real OS signal delivery
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
