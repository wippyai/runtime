package otel

import "github.com/wippyai/runtime/api/boot"

const (
	OTelName            boot.Name = "otel"
	OTelHTTPName        boot.Name = "otel-http"
	OTelProcessName     boot.Name = "otel-process"
	OTelInterceptorName boot.Name = "otel-interceptor"
	OTelQueueName       boot.Name = "otel-queue"
	OTelMetricsName     boot.Name = "otel-metrics"

	httpName         boot.Name = "http"
	processName      boot.Name = "process"
	interceptorName  boot.Name = "interceptor"
	queueManagerName boot.Name = "queues"
	metricsName      boot.Name = "metrics"
)
