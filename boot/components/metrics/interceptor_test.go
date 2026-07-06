// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/telemetrytest"
	metricsinterceptor "github.com/wippyai/runtime/service/metrics/interceptor"
	"go.uber.org/zap"
)

type mockInterceptorRegistry struct {
	registrations []registeredInterceptor
}

type registeredInterceptor struct {
	interceptor function.Interceptor
	name        string
	order       int
}

func (m *mockInterceptorRegistry) Register(name string, i function.Interceptor, order int) error {
	m.registrations = append(m.registrations, registeredInterceptor{name: name, interceptor: i, order: order})
	return nil
}

func (m *mockInterceptorRegistry) Unregister(string) error { return nil }

func (m *mockInterceptorRegistry) Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
	return f(ctx, task)
}

func TestMetricsInterceptor_RegistersWhenEnabled(t *testing.T) {
	component := Interceptor()
	assert.Equal(t, InterceptorName, component.Name())

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx = boot.WithConfig(ctx, boot.NewConfig(boot.WithSection("metrics", map[string]any{
		"interceptor.enabled": true,
	})))
	rec := telemetrytest.NewRecorder()
	ctx = metricsapi.WithCollector(ctx, rec)
	reg := &mockInterceptorRegistry{}
	ctx = function.WithInterceptorRegistry(ctx, reg)

	newCtx, err := component.Load(ctx)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
	require.Len(t, reg.registrations, 1)

	r := reg.registrations[0]
	assert.Equal(t, "metrics", r.name)
	assert.Equal(t, 50, r.order)

	fi, ok := r.interceptor.(*metricsinterceptor.FunctionInterceptor)
	require.True(t, ok, "registered interceptor must be *FunctionInterceptor")

	funcID := registry.NewID("ns", "test_func")
	task := runtime.Task{ID: funcID}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}
	_, err = fi.Handle(context.Background(), task, next)
	require.NoError(t, err)
	assert.Equal(t, 1.0, rec.CounterValue(metricsinterceptor.FunctionCalls,
		metricsapi.Labels{"function_id": funcID.String(), "status": "success"}))
}

func TestMetricsInterceptor_NotRegisteredWhenDisabled(t *testing.T) {
	component := Interceptor()

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx = boot.WithConfig(ctx, boot.NewConfig(boot.WithSection("metrics", map[string]any{
		"interceptor.enabled": false,
	})))
	ctx = metricsapi.WithCollector(ctx, telemetrytest.NewRecorder())
	reg := &mockInterceptorRegistry{}
	ctx = function.WithInterceptorRegistry(ctx, reg)

	_, err := component.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, reg.registrations)
}

func TestMetricsInterceptor_NoCollector(t *testing.T) {
	component := Interceptor()

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx = boot.WithConfig(ctx, boot.NewConfig(boot.WithSection("metrics", map[string]any{
		"interceptor.enabled": true,
	})))
	reg := &mockInterceptorRegistry{}
	ctx = function.WithInterceptorRegistry(ctx, reg)

	_, err := component.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, reg.registrations, "no registration without a collector")
}
