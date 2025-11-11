package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	contractsys "github.com/ponyruntime/pony/system/contract"
)

type contractsPlugin struct{}

func (p *contractsPlugin) Name() string      { return bootpkg.Contracts }
func (p *contractsPlugin) Phase() boot.Phase { return boot.Init }
func (p *contractsPlugin) DependsOn() []string {
	return []string{bootpkg.EventBus, bootpkg.Logger, bootpkg.Functions}
}

func (p *contractsPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)
	funcs := funcapi.GetRegistry(ctx)

	contractRegistry := contractsys.NewContractRegistry(bus, logger.Named("contracts"))
	contractInstantiator := contractsys.NewContractInstantiator(contractRegistry, funcs)

	ctx = contract.WithContracts(ctx, contractRegistry, contractInstantiator)

	return ctx, nil
}

func (p *contractsPlugin) Start(ctx context.Context) error {
	contractRegistry, _ := contract.GetContracts(ctx)
	return contractRegistry.Start(ctx)
}

func (p *contractsPlugin) Stop(ctx context.Context) error {
	contractRegistry, _ := contract.GetContracts(ctx)
	return contractRegistry.Stop()
}

func init() {
	bootpkg.MustRegister(&contractsPlugin{})
}
