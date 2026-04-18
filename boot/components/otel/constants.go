// SPDX-License-Identifier: MPL-2.0

package otel

import "github.com/wippyai/runtime/api/boot"

const (
	Name            boot.Name = "otel"
	HTTPName        boot.Name = "otel-http"
	ProcessName     boot.Name = "otel-process"
	InterceptorName boot.Name = "otel-interceptor"
	QueueName       boot.Name = "otel-queue"
	MetricsName     boot.Name = "otel-metrics"
	TemporalName    boot.Name = "otel-temporal"

	httpName         boot.Name = "http"
	interceptorName  boot.Name = "interceptor"
	queueManagerName boot.Name = "queue"
	metricsName      boot.Name = "metrics"
)
