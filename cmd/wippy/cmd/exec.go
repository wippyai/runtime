// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// newExecSignalContext gives an in-flight CLI exec its own interrupt-aware
// context. The regular supervisor signal channel remains untouched so the
// outer run loop can still perform the normal graceful shutdown after the
// child has been canceled.
func newExecSignalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	execCtx, cancel := newExecSignalContextWithChannel(ctx, sigChan)
	go func() {
		<-execCtx.Done()
		signal.Stop(sigChan)
	}()

	return execCtx, func() {
		cancel()
		signal.Stop(sigChan)
	}
}

// newExecSignalContextWithChannel is the signal-independent part of
// newExecSignalContext. Keeping the channel injectable makes cancellation
// behavior testable on platforms where sending an OS signal is unsupported.
func newExecSignalContextWithChannel(ctx context.Context, sigChan <-chan os.Signal) (context.Context, context.CancelFunc) {
	execCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
		case <-execCtx.Done():
		}
	}()

	return execCtx, cancel
}

// execWasInterrupted reports a user interrupt without mistaking cancellation
// inherited from the parent runtime context for Ctrl-C.
func execWasInterrupted(execCtx, parentCtx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && parentCtx.Err() == nil && execCtx.Err() != nil
}

type execCompletion struct {
	data any
	err  error
}

type execResultReceiver struct {
	done chan execCompletion
	once sync.Once
}

func (r *execResultReceiver) CompleteYield(_ uint64, data any, err error) {
	r.once.Do(func() {
		r.done <- execCompletion{data: data, err: err}
	})
}

// waitForExecResult dispatches process.exec and does not return until the
// launched process has produced an ExecResult or the caller is canceled. The
// command is intentionally not pooled here: a cancellation may race with a
// handler's final callback, and retaining this small command until the
// callback is harmless while avoiding use-after-release hazards.
func waitForExecResult(ctx context.Context, command *process.ExecCmd) (*runtime.Result, error) {
	d := dispatcher.GetDispatcher(ctx)
	if d == nil {
		return nil, fmt.Errorf("process dispatcher not available")
	}
	handler := d.Dispatch(command)
	if handler == nil {
		return nil, fmt.Errorf("process exec handler not available")
	}

	receiver := &execResultReceiver{done: make(chan execCompletion, 1)}
	if err := handler.Handle(ctx, command, 1, receiver); err != nil {
		return nil, err
	}

	select {
	case completion := <-receiver.done:
		if completion.err != nil {
			return nil, completion.err
		}
		result, ok := completion.data.(process.ExecResult)
		if !ok {
			return nil, fmt.Errorf("process exec returned %T, want process.ExecResult", completion.data)
		}
		return result.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
