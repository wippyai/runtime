// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
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
