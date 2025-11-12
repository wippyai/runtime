package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	contractsys "github.com/ponyruntime/pony/system/contract"
)

func Contracts() boot.Plugin {
	var contractRegistry *contractsys.Registry

	return boot.New(boot.P{
		Name:      ContractsName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger", "functions"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			funcs := funcapi.GetRegistry(ctx)

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
		Stop: func(ctx context.Context) error {
			if contractRegistry != nil {
				return contractRegistry.Stop()
			}
			return nil
		},
	})
}
