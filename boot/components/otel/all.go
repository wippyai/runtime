package otel

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		OTel(),
		OTelHTTP(),
		OTelProcess(),
		OTelInterceptor(),
		OTelQueue(),
		OTelMetrics(),
	}
}
