package wasm

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys.
var (
	TransportRegistryKey = &ctxapi.Key{Name: "wasm.transportRegistry"}
	AsyncStateKey        = &ctxapi.Key{Name: "wasm.asyncState", Inherit: false}
)

// TransportRegistry resolves transports by name.
type TransportRegistry interface {
	Get(name string) (any, bool)
}

// AsyncState tracks pending async execution state for a frame.
type AsyncState struct {
	Payload any    `json:"payload,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Waiting bool   `json:"waiting,omitempty"`
}

// SetTransportRegistry stores transport registry in AppContext.
func SetTransportRegistry(ctx context.Context, tr TransportRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(TransportRegistryKey) == nil {
		ac.With(TransportRegistryKey, tr)
	}
	return ctx
}

// GetTransportRegistry retrieves transport registry from AppContext.
func GetTransportRegistry(ctx context.Context) TransportRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if tr, ok := ac.Get(TransportRegistryKey).(TransportRegistry); ok {
		return tr
	}
	return nil
}

// SetAsyncState stores async state in FrameContext.
func SetAsyncState(ctx context.Context, st *AsyncState) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(AsyncStateKey, st)
}

// GetAsyncState retrieves async state from FrameContext.
func GetAsyncState(ctx context.Context) *AsyncState {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if v, ok := fc.Get(AsyncStateKey); ok {
		st, _ := v.(*AsyncState)
		return st
	}
	return nil
}
