package otel

import "github.com/wippyai/runtime/api/boot"

const (
	OTelName            boot.ComponentName = "otel"
	OTelHTTPName        boot.ComponentName = "otel-http"
	OTelProcessName     boot.ComponentName = "otel-process"
	OTelInterceptorName boot.ComponentName = "otel-interceptor"
	OTelQueueName       boot.ComponentName = "otel-queue"
	OTelMetricsName     boot.ComponentName = "otel-metrics"

	httpName         boot.ComponentName = "http"
	processName      boot.ComponentName = "process"
	interceptorName  boot.ComponentName = "interceptor"
	queueManagerName boot.ComponentName = "queues"
	metricsName      boot.ComponentName = "metrics"
)
