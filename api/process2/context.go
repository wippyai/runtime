package process2

import (
	"context"
	"errors"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing process2-related information
var (
	managerCtx         = &ctxapi.Key{Name: "process2.manager"}
	onStartHooksCtx    = &ctxapi.Key{Name: "process2.onStartHooks"}
	onCompleteHooksCtx = &ctxapi.Key{Name: "process2.onCompleteHooks"}
)

// WithManager attaches a process Manager to the context.
func WithManager(ctx context.Context, m Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerCtx) == nil {
		ac.With(managerCtx, m)
	}
	return ctx
}

// GetManager retrieves the process Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerCtx); val != nil {
		if m, ok := val.(Manager); ok {
			return m
		}
	}
	return nil
}

// SetOnStartHooks sets OnStart hook arrays in the FrameContext.
func SetOnStartHooks(ctx context.Context, hooks []OnStart) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}
	return fc.Set(onStartHooksCtx, hooks)
}

// GetOnStartHooks retrieves the OnStart hook array from the FrameContext.
func GetOnStartHooks(ctx context.Context) []OnStart {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onStartHooksCtx); ok {
		if hooks, ok := val.([]OnStart); ok {
			return hooks
		}
	}
	return nil
}

// SetOnCompleteHooks sets OnComplete hook arrays in the FrameContext.
func SetOnCompleteHooks(ctx context.Context, hooks []OnComplete) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}
	return fc.Set(onCompleteHooksCtx, hooks)
}

// GetOnCompleteHooks retrieves the OnComplete hook array from the FrameContext.
func GetOnCompleteHooks(ctx context.Context) []OnComplete {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onCompleteHooksCtx); ok {
		if hooks, ok := val.([]OnComplete); ok {
			return hooks
		}
	}
	return nil
}
