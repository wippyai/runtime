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
// Defensive: returns ctx unchanged if AppContext is nil or logger already exists.
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(loggerCtx) == nil {
		ac.With(loggerCtx, logger)
	}
	return ctx
}

// UpdateLogger replaces the logger in the context with a new instance.
// This is used when wrapping the logger with additional functionality (e.g., event streaming).
// Defensive: returns ctx unchanged if AppContext is nil.
func UpdateLogger(ctx context.Context, logger *zap.Logger) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	ac.Update(loggerCtx, logger)
	return ctx
}

// GetLogger retrieves the logger instance from the provided context.
// If no logger is found in the context, it returns a no-op logger.
// This ensures that logging calls will not panic when no logger is configured.
// Defensive: returns no-op logger if AppContext is nil.
func GetLogger(ctx context.Context) *zap.Logger {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return zap.NewNop()
	}
	if l := ac.Get(loggerCtx); l != nil {
		return l.(*zap.Logger)
	}
	return zap.NewNop()
}
