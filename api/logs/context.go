// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.uber.org/zap"
)

var (
	loggerKey  = &ctxapi.Key{Name: "logger"}
	managerKey = &ctxapi.Key{Name: "logs.manager"}
)

// WithLogger attaches a logger to the context.
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(loggerKey) == nil {
		ac.With(loggerKey, logger)
	}
	return ctx
}

// GetLogger retrieves the logger from the context. Returns a no-op logger if not found.
func GetLogger(ctx context.Context) *zap.Logger {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return zap.NewNop()
	}
	if l := ac.Get(loggerKey); l != nil {
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
	if ac.Get(managerKey) == nil {
		ac.With(managerKey, mgr)
	}
	return ctx
}

// GetManager retrieves the Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerKey); val != nil {
		if mgr, ok := val.(Manager); ok {
			return mgr
		}
	}
	return nil
}
