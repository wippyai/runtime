package logs

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.uber.org/zap"
)

var (
	loggerCtx  = &ctxapi.Key{Name: "logger"}
	managerCtx = &ctxapi.Key{Name: "logs.manager"}
)

// WithLogger attaches a logger to the context.
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

// UpdateLogger replaces the logger in the context.
func UpdateLogger(ctx context.Context, logger *zap.Logger) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	ac.Update(loggerCtx, logger)
	return ctx
}

// GetLogger retrieves the logger from the context. Returns a no-op logger if not found.
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

// WithManager attaches a Manager to the context.
func WithManager(ctx context.Context, mgr Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerCtx) == nil {
		ac.With(managerCtx, mgr)
	}
	return ctx
}

// GetManager retrieves the Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerCtx); val != nil {
		if mgr, ok := val.(Manager); ok {
			return mgr
		}
	}
	return nil
}
