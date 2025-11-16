package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/system/process"
)

func ProcessTopology() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessTopologyName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			manager := procapi.GetManager(ctx)
			if manager == nil {
				return ctx, fmt.Errorf("process manager not available in context")
			}

			processManager, ok := manager.(*process.Manager)
			if !ok {
				return ctx, fmt.Errorf("process manager is not *process.Manager")
			}

			processManager.RegisterMutator(process.TopologyLifecycleMutator(logger.Named("topology-mutator")))

			return ctx, nil
		},
		Start: func(_ context.Context) error {
			return nil
		},
		Stop: func(_ context.Context) error {
			return nil
		},
	})
}
