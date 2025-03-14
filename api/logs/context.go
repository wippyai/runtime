// Package logs provides a structured logging system with context integration.
package logs

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"go.uber.org/zap"
)

// loggerCtx is the context key used to store and retrieve the logger instance
var loggerCtx = &ctxapi.Key{Name: "logger"} //nolint:gochecknoglobals

// WithLogger creates a new context with the provided logger instance.
// This function is used to inject a logger into the context for later retrieval.
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerCtx, logger)
}

// GetLogger retrieves the logger instance from the provided context.
// If no logger is found in the context, it returns a no-op logger.
// This ensures that logging calls will not panic when no logger is configured.
func GetLogger(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerCtx).(*zap.Logger); ok {
		return l
	}

	return zap.NewNop()
}
