// SPDX-License-Identifier: MPL-2.0

package temporal

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
)

// Context keys for storing temporal-related data
var (
	activityContextKey      = &ctxapi.Key{Name: "temporal.activity.context"}
	clientIDKey             = &ctxapi.Key{Name: "temporal.client.id", Inherit: true}
	workerIDKey             = &ctxapi.Key{Name: "temporal.worker.id", Inherit: true}
	clientInterceptorRegKey = &ctxapi.Key{Name: "temporal.interceptor.client"}
	workerInterceptorRegKey = &ctxapi.Key{Name: "temporal.interceptor.worker"}
	dataConverterRegKey     = &ctxapi.Key{Name: "temporal.dataconverter.registry"}
	runHandoffRegKey        = &ctxapi.Key{Name: "temporal.run.handoff.registry"}
)

// ClientInterceptorRegistry provides methods to register client interceptors
type ClientInterceptorRegistry interface {
	Register(i interceptor.ClientInterceptor)
	GetAll() []interceptor.ClientInterceptor
}

// WorkerInterceptorRegistry provides methods to register worker interceptors
type WorkerInterceptorRegistry interface {
	Register(i interceptor.WorkerInterceptor)
	GetAll() []interceptor.WorkerInterceptor
}

// DataConverterRegistry provides methods to register payload codecs.
type DataConverterRegistry interface {
	RegisterCodec(codec converter.PayloadCodec)
	Build() converter.DataConverter
}

// WorkflowRunHandoff stores one-shot workflow run metadata between start and monitor/link setup.
type WorkflowRunHandoff interface {
	Publish(clientID, workflowID, runID string)
	Consume(clientID, workflowID string) (runID string, ok bool)
}

// WithClientInterceptorRegistry stores the client interceptor registry in context
func WithClientInterceptorRegistry(ctx context.Context, reg ClientInterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return context.WithValue(ctx, clientInterceptorRegKey, reg)
	}
	if ac.Get(clientInterceptorRegKey) == nil {
		ac.With(clientInterceptorRegKey, reg)
	}
	return ctx
}

// GetClientInterceptorRegistry retrieves the client interceptor registry from context
func GetClientInterceptorRegistry(ctx context.Context) ClientInterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if val := ac.Get(clientInterceptorRegKey); val != nil {
			if reg, ok := val.(ClientInterceptorRegistry); ok {
				return reg
			}
		}
	}
	if val := ctx.Value(clientInterceptorRegKey); val != nil {
		if reg, ok := val.(ClientInterceptorRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithWorkerInterceptorRegistry stores the worker interceptor registry in context
func WithWorkerInterceptorRegistry(ctx context.Context, reg WorkerInterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return context.WithValue(ctx, workerInterceptorRegKey, reg)
	}
	if ac.Get(workerInterceptorRegKey) == nil {
		ac.With(workerInterceptorRegKey, reg)
	}
	return ctx
}

// GetWorkerInterceptorRegistry retrieves the worker interceptor registry from context
func GetWorkerInterceptorRegistry(ctx context.Context) WorkerInterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if val := ac.Get(workerInterceptorRegKey); val != nil {
			if reg, ok := val.(WorkerInterceptorRegistry); ok {
				return reg
			}
		}
	}
	if val := ctx.Value(workerInterceptorRegKey); val != nil {
		if reg, ok := val.(WorkerInterceptorRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithDataConverterRegistry stores the data converter registry in context.
func WithDataConverterRegistry(ctx context.Context, reg DataConverterRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return context.WithValue(ctx, dataConverterRegKey, reg)
	}
	if ac.Get(dataConverterRegKey) == nil {
		ac.With(dataConverterRegKey, reg)
	}
	return ctx
}

// GetDataConverterRegistry retrieves the data converter registry from context.
func GetDataConverterRegistry(ctx context.Context) DataConverterRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if val := ac.Get(dataConverterRegKey); val != nil {
			if reg, ok := val.(DataConverterRegistry); ok {
				return reg
			}
		}
	}
	if val := ctx.Value(dataConverterRegKey); val != nil {
		if reg, ok := val.(DataConverterRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithWorkflowRunHandoff stores the workflow run handoff registry in context.
func WithWorkflowRunHandoff(ctx context.Context, reg WorkflowRunHandoff) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return context.WithValue(ctx, runHandoffRegKey, reg)
	}
	if ac.Get(runHandoffRegKey) == nil {
		ac.With(runHandoffRegKey, reg)
	}
	return ctx
}

// GetWorkflowRunHandoff retrieves the workflow run handoff registry from context.
func GetWorkflowRunHandoff(ctx context.Context) WorkflowRunHandoff {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil {
		if val := ac.Get(runHandoffRegKey); val != nil {
			if reg, ok := val.(WorkflowRunHandoff); ok {
				return reg
			}
		}
	}
	if val := ctx.Value(runHandoffRegKey); val != nil {
		if reg, ok := val.(WorkflowRunHandoff); ok {
			return reg
		}
	}
	return nil
}

// ActivityContextKey returns the context key for activity context storage
func ActivityContextKey() *ctxapi.Key {
	return activityContextKey
}

// WithClientID stores the temporal client ID in context for peer routing
func WithClientID(ctx context.Context, clientID string) context.Context {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if err := fc.Set(clientIDKey, clientID); err == nil {
			return ctx
		}
	}
	return context.WithValue(ctx, clientIDKey, clientID)
}

// GetClientID retrieves the temporal client ID from context
func GetClientID(ctx context.Context) string {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if val, ok := fc.Get(clientIDKey); ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
	}
	if val := ctx.Value(clientIDKey); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// WithWorkerID stores the temporal worker ID in context for workflow PID host routing.
func WithWorkerID(ctx context.Context, workerID string) context.Context {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if err := fc.Set(workerIDKey, workerID); err == nil {
			return ctx
		}
	}
	return context.WithValue(ctx, workerIDKey, workerID)
}

// GetWorkerID retrieves the temporal worker ID from context.
func GetWorkerID(ctx context.Context) string {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if val, ok := fc.Get(workerIDKey); ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
	}
	if val := ctx.Value(workerIDKey); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
