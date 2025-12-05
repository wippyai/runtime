package system

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	contractsys "github.com/wippyai/runtime/system/contract"
	"go.uber.org/zap"
)

func Contracts() boot.Component {
	var contractRegistry *contractsys.Registry

	return boot.New(boot.P{
		Name:      ContractsName,
		DependsOn: []boot.Name{bootcore.RegistryName, FunctionsName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("contracts")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			funcs := funcapi.GetRegistry(ctx)
			if funcs == nil {
				return ctx, ErrFunctionRegistryNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register contract dependency patterns
			contractPatterns := []regapi.DependencyPattern{
				{Path: "data.contracts.*.contract", Description: "Contract definition references in bindings", AllowWildcard: true},
				{Path: "data.contracts.*.methods.*", Description: "Method implementation function references in bindings", AllowWildcard: true},
			}
			for _, pattern := range contractPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register contract dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			contractRegistry = contractsys.NewContractRegistry(bus, logger.Named("contracts"))
			contractInstantiator := contractsys.NewContractInstantiator(contractRegistry, funcs)

			ctx = contract.WithContracts(ctx, contractRegistry, contractInstantiator)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if contractRegistry != nil {
				return contractRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if contractRegistry != nil {
				return contractRegistry.Stop()
			}
			return nil
		},
	})
}
