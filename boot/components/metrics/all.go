// SPDX-License-Identifier: MPL-2.0

package metrics

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Metrics(),
		// MetricsInterceptor(),
	}
}
