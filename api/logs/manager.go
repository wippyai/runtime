package logs

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// LogManager represents a logging manager that can be started and stopped.
// This interface abstracts the concrete Manager type from system/logs.
type LogManager interface {
	Start(ctx context.Context) error
	Stop() error
	GetConfig() Config
}

var logManagerCtx = &ctxapi.Key{Name: "logs.manager"}

// WithLogManager attaches a LogManager instance to the provided context.
func WithLogManager(ctx context.Context, mgr LogManager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(logManagerCtx) == nil {
		ac.With(logManagerCtx, mgr)
	}
	return ctx
}

// GetLogManager retrieves the LogManager instance from the provided context.
func GetLogManager(ctx context.Context) LogManager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(logManagerCtx); val != nil {
		if mgr, ok := val.(LogManager); ok {
			return mgr
		}
	}
	return nil
}
