// SPDX-License-Identifier: MPL-2.0

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
	"github.com/wippyai/runtime/boot/components/service/temporal"
)

func All() []boot.Component {
	components := []boot.Component{
		Network(),
		fs.Directory(),
		fs.Embed(),
		Template(),
		Terminal2(),
		Exec(),
		Host(),
		ProcessSupervisor(),
		ProcessFunc(),
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
