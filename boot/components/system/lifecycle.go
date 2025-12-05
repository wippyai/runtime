package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	processapi "github.com/wippyai/runtime/api/process"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/topology"
)

const LifecycleName = "system.lifecycle"

func Lifecycle() boot.Component {
	return boot.New(boot.P{
		Name:      LifecycleName,
		DependsOn: []boot.Name{TopologyName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("lifecycle")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			topo := topapi.GetTopology(ctx)
			if topo == nil {
				return ctx, ErrTopologyNotAvailable
			}

			pidReg := topapi.GetRegistry(ctx)

			lifecycleReg := process.NewLifecycleRegistry()
			topoLifecycle := topology.NewLifecycle(topo, pidReg, logger.Named("topology.lifecycle"))
			lifecycleReg.Register("topology", topoLifecycle)

			ctx = processapi.WithLifecycleRegistry(ctx, lifecycleReg)

			logger.Info("lifecycle registry initialized with topology lifecycle")
			return ctx, nil
		},
	})
}
