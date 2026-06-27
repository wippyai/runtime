// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	processapi "github.com/wippyai/runtime/api/process"
	impl "github.com/wippyai/runtime/service/metrics"
)

// Process registers the process lifecycle metrics handler, which emits
// wippy_process_started_total / wippy_process_terminated_total /
// wippy_process_active alongside the existing OTel lifecycle spans.
func Process() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessName,
		DependsOn: []boot.Name{Name, lifecycleName},
		Load: func(ctx context.Context) (context.Context, error) {
			coll := metricsapi.GetCollector(ctx)
			if coll == nil {
				return ctx, nil
			}

			reg := processapi.GetLifecycleRegistry(ctx)
			if reg == nil {
				return ctx, nil
			}

			reg.Register("metrics", impl.NewProcessLifecycle(coll))
			return ctx, nil
		},
	})
}
