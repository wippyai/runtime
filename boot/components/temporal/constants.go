package temporal

import "github.com/wippyai/runtime/api/boot"

const (
	ClientInterceptorName boot.ComponentName = "temporal.client.interceptor"
	WorkerInterceptorName boot.ComponentName = "temporal.worker.interceptor"
	DataConverterName     boot.ComponentName = "temporal.dataconverter"
	ClientManagerName     boot.ComponentName = "temporal.client.manager"
	WorkerManagerName     boot.ComponentName = "temporal.worker.manager"
	ActivityListenerName  boot.ComponentName = "temporal.activity.listener"
)
