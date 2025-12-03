package supervisor

import (
	"context"
	"os"
	"syscall"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	exitCodeCtxKey      = &ctxapi.Key{Name: "supervisor.exitCodeCtxKey"}
	signalChannelCtxKey = &ctxapi.Key{Name: "supervisor.signalChannelCtxKey"}
)

func setExitCode(ctx context.Context, code int) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		ac.Update(exitCodeCtxKey, code)
	}
}

// GetExitCode retrieves the exit code from the application context.
func GetExitCode(ctx context.Context) int {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return 0
	}
	if code := ac.Get(exitCodeCtxKey); code != nil {
		if c, ok := code.(int); ok {
			return c
		}
	}
	return 0
}

// SetSignalChannel stores the signal channel in the application context.
func SetSignalChannel(ctx context.Context, ch chan<- os.Signal) {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		ac.Update(signalChannelCtxKey, ch)
	}
}

func getSignalChannel(ctx context.Context) chan<- os.Signal {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if ch := ac.Get(signalChannelCtxKey); ch != nil {
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
