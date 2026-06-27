// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	metricsinterceptor "github.com/wippyai/runtime/service/metrics/interceptor"
)

// Interceptor wires the function-call metrics interceptor
// (wippy_function_calls / wippy_function_duration / wippy_function_in_flight)
// into the function interceptor registry. It runs at order 50 so the duration
// timer wraps the full call, including the OTel tracing interceptor (order 100).
func Interceptor() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorName,
		DependsOn: []boot.Name{Name, interceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			if !loadInterceptorEnabled(ctx) {
				return ctx, nil
			}

			collector := metricsapi.GetCollector(ctx)
			if collector == nil {
				return ctx, nil
			}

			registry := apiinterceptor.GetInterceptorRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			if err := registry.Register("metrics", metricsinterceptor.NewFunctionInterceptor(collector, true), 50); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}

// loadInterceptorEnabled reads metrics.interceptor.enabled, defaulting to true
// so function-call metrics emit out of the box.
func loadInterceptorEnabled(ctx context.Context) bool {
	bootCfg := boot.GetConfig(ctx)
	if bootCfg == nil {
		return true
	}

	metricsCfg := bootCfg.Sub("metrics")
	if metricsCfg == nil {
		return true
	}

	return metricsCfg.GetBool("interceptor.enabled", true)
}
