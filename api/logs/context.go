// Package logs provides a structured logging system with context integration.
package logs

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"go.uber.org/zap"
)

// loggerCtx is the context key used to store and retrieve the logger instance
var loggerCtx = &ctxapi.Key{Name: "logger"}

// WithLogger creates a new context with the provided logger instance.
// This function is used to inject a logger into the context for later retrieval.
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if ac.Get(loggerCtx) == nil {
			ac.With(loggerCtx, logger)
		}
	}
	return ctx
}

// GetLogger retrieves the logger instance from the provided context.
// If no logger is found in the context, it returns a no-op logger.
// This ensures that logging calls will not panic when no logger is configured.
func GetLogger(ctx context.Context) *zap.Logger {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if l := ac.Get(loggerCtx); l != nil {
			return l.(*zap.Logger)
		}
	}
	return zap.NewNop()
}
