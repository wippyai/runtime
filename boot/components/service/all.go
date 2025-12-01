package service

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/components/env"
	"github.com/wippyai/runtime/boot/components/metrics"
	"github.com/wippyai/runtime/boot/components/otel"
	"github.com/wippyai/runtime/boot/components/prometheus"
	"github.com/wippyai/runtime/boot/components/service/aws"
	"github.com/wippyai/runtime/boot/components/service/fs"
	"github.com/wippyai/runtime/boot/components/service/storage"
	"github.com/wippyai/runtime/boot/components/temporal"
)

func All() []boot.Component {
	components := []boot.Component{
		fs.Directory(),
		fs.Embed(),
		Template(),
		Terminal(),
		Exec(),
		Host2(),
		Policy(),
		Contract(),
		HTTP(),
		InterceptorRetry(),
	}

	components = append(components, env.All()...)
	components = append(components, storage.All()...)
	components = append(components, aws.All()...)
	components = append(components, metrics.All()...)
	components = append(components, prometheus.All()...)
	components = append(components, otel.All()...)
	components = append(components, temporal.All()...)

	return components
}
