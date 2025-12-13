package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/processfunc"
)

// ProcessFunc creates a boot component that bridges process.* registry
// entries to function handlers when they have default_host configured.
func ProcessFunc() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessFuncName,
		DependsOn: []boot.Name{system.FunctionsName, system.TopologyName, system.ProcessManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			pidGen := process.GetPIDGenerator(ctx)
			if pidGen == nil {
				return ctx, ErrPIDGeneratorNotAvailable
			}

			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailable
			}

			topo := topology.GetTopology(ctx)
			if topo == nil {
				return ctx, ErrTopologyNotAvailable
			}

			manager := process.GetManager(ctx)
			if manager == nil {
				return ctx, ErrProcessManagerNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			listener := processfunc.NewListener(
				logger.Named("processfunc"),
				bus,
				pidGen,
				node,
				topo,
				manager,
			)

			// Register as observer - pfunc is secondary, should not send Accept/Reject
			handlers.RegisterObserver("process.*", listener)

			logger.Info("process function bridge registered")
			return ctx, nil
		},
	})
}
