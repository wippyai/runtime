package supervisor

import (
	"context"
	"os"
	"sync/atomic"
	"syscall"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	signalChannelKey = &ctxapi.Key{Name: "supervisor.signal"}
	// exitCode is stored atomically since it's set at runtime during shutdown
	exitCode atomic.Int32
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

// SetSignalChannel stores the signal channel in the application context.
// Must be called during boot before AppContext is sealed.
func SetSignalChannel(ctx context.Context, ch chan<- os.Signal) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		ac.With(signalChannelKey, ch)
	}
}

func getSignalChannel(ctx context.Context) chan<- os.Signal {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if ch := ac.Get(signalChannelKey); ch != nil {
		if c, ok := ch.(chan<- os.Signal); ok {
			return c
		}
	}
	return nil
}

// TriggerShutdown sets the exit code and sends a SIGTERM signal to trigger
// graceful application shutdown.
func TriggerShutdown(ctx context.Context, code int) {
	setExitCode(code)
	if ch := getSignalChannel(ctx); ch != nil {
		ch <- syscall.SIGTERM
	}
}
