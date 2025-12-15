package otel

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		OTel(),
		HTTP(),
		Interceptor(),
		Queue(),
		Metrics(),
		ProcessLifecycle(),
	}
}
