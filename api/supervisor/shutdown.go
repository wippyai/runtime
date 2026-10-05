// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	shutdownRequestKey = &ctxapi.Key{Name: "supervisor.shutdown_requests"}
	// exitCode is stored atomically since it's set at runtime during shutdown
	exitCode     atomic.Int32
	shutdownSent atomic.Bool
	shutdownMu   sync.Mutex
)

func setExitCode(code int) {
	if code < -2147483648 || code > 2147483647 {
		code = 1
	}
	exitCode.Store(int32(code))
}

// GetExitCode retrieves the exit code.
func GetExitCode() int {
	return int(exitCode.Load())
}

// SetShutdownRequestChannel stores the channel that receives programmatic
// shutdown requests in the application context. It carries requests only; OS
// termination signals travel on their own channel owned by the process runner.
// Must be called during boot before AppContext is sealed.
func SetShutdownRequestChannel(ctx context.Context, ch chan<- struct{}) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		shutdownMu.Lock()
		defer shutdownMu.Unlock()
		setExitCode(0)
		shutdownSent.Store(false)
		ac.With(shutdownRequestKey, ch)
	}
}

func getShutdownRequestChannel(ctx context.Context) chan<- struct{} {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if ch := ac.Get(shutdownRequestKey); ch != nil {
		if c, ok := ch.(chan<- struct{}); ok {
			return c
		}
	}
	return nil
}

// TriggerShutdown sets the exit code and sends a shutdown request to trigger
// graceful application shutdown. Only the first call sends the request;
// subsequent calls update the exit code but do not send duplicate requests.
func TriggerShutdown(ctx context.Context, code int) {
	shutdownMu.Lock()
	setExitCode(code)
	if !shutdownSent.CompareAndSwap(false, true) {
		shutdownMu.Unlock()
		return
	}
	ch := getShutdownRequestChannel(ctx)
	shutdownMu.Unlock()
	if ch != nil {
		ch <- struct{}{}
	}
}

// TriggerShutdownIfIdle requests shutdown for a completed command only when
// another process has not already requested a code. A command cancelled during
// shutdown must not replace that process's exit code with its own result.
func TriggerShutdownIfIdle(ctx context.Context, code int) {
	shutdownMu.Lock()
	if !shutdownSent.CompareAndSwap(false, true) {
		shutdownMu.Unlock()
		return
	}
	setExitCode(code)
	ch := getShutdownRequestChannel(ctx)
	shutdownMu.Unlock()
	if ch != nil {
		ch <- struct{}{}
	}
}
