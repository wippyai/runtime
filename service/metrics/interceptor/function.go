// SPDX-License-Identifier: MPL-2.0

package interceptor

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/runtime"
)

const (
	FunctionCalls    = "wippy_function_calls"
	FunctionDuration = "wippy_function_duration"
	FunctionInFlight = "wippy_function_in_flight"
)

type FunctionInterceptor struct {
	collector metrics.Collector
	enabled   bool
}

func NewFunctionInterceptor(collector metrics.Collector, enabled bool) *FunctionInterceptor {
	return &FunctionInterceptor{
		collector: collector,
		enabled:   enabled,
	}
}

func (i *FunctionInterceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	if !i.enabled || i.collector == nil {
		return next(ctx, task)
	}

	funcID := task.ID.String()
	baseLabels := metrics.Labels{"function_id": funcID}

	i.collector.GaugeInc(FunctionInFlight, baseLabels)
	start := time.Now()

	result, err := next(ctx, task)

	duration := time.Since(start).Seconds()
	i.collector.GaugeDec(FunctionInFlight, baseLabels)
	i.collector.HistogramObserve(FunctionDuration, duration, baseLabels)

	var status string
	if err != nil || (result != nil && result.Error != nil) {
		status = "error"
	} else {
		status = "success"
	}
	i.collector.CounterInc(FunctionCalls, metrics.Labels{"function_id": funcID, "status": status})

	return result, err
}
