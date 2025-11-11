package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/function"
)

type functionsPlugin struct{}

func (p *functionsPlugin) Name() string      { return bootpkg.Functions }
func (p *functionsPlugin) Phase() boot.Phase { return boot.Init }
func (p *functionsPlugin) DependsOn() []string {
	return []string{bootpkg.EventBus, bootpkg.Logger, bootpkg.PubSub}
}

func (p *functionsPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)
	node := pubsubapi.GetNode(ctx)

	funcHost, err := node.Host(funcapi.HostID)
	if err != nil {
		return ctx, err
	}

	funcs := function.NewFunctionRegistry(bus, funcHost, logger.Named("funcs"))
	return funcapi.WithRegistry(ctx, funcs), nil
}

func (p *functionsPlugin) Start(ctx context.Context) error {
	funcs := funcapi.GetRegistry(ctx)
	return funcs.Start(ctx)
}

func (p *functionsPlugin) Stop(ctx context.Context) error {
	funcs := funcapi.GetRegistry(ctx)
	return funcs.Stop()
}

func init() {
	bootpkg.MustRegister(&functionsPlugin{})
}
