package system

import (
	"context"
	"os"
	"syscall"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var exitCodeKey = &ctxapi.Key{Name: "system.exitcode"}
var signalChannelKey = &ctxapi.Key{Name: "system.sigchan"}

// setExitCode stores the exit code in the application context.
// This code will be used when the application exits.
// This is an internal function, use TriggerShutdown for external calls.
func setExitCode(ctx context.Context, code int) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		ac.Update(exitCodeKey, code)
	}
}

// GetExitCode retrieves the exit code from the application context.
// Returns 0 if no exit code has been set.
func GetExitCode(ctx context.Context) int {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return 0
	}
	if code := ac.Get(exitCodeKey); code != nil {
		if c, ok := code.(int); ok {
			return c
		}
	}
	return 0
}

// SetSignalChannel stores the signal channel in the application context.
// This allows services to trigger shutdown programmatically.
func SetSignalChannel(ctx context.Context, ch chan<- os.Signal) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		ac.Update(signalChannelKey, ch)
	}
}

// getSignalChannel retrieves the signal channel from the application context.
// Returns nil if no signal channel has been set.
// This is an internal function, use TriggerShutdown for external calls.
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
	setExitCode(ctx, code)

	if ch := getSignalChannel(ctx); ch != nil {
		ch <- syscall.SIGTERM
	}
}
