package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/system/function"
)

func Functions() boot.Component {
	var funcs *function.Registry

	return boot.New(boot.P{
		Name:      FunctionsName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			node := pubsubapi.GetNode(ctx)

			if node != nil {
				if err := node.RegisterHost(funcapi.HostID, node); err != nil {
					return ctx, err
				}

				funcs = function.NewFunctionRegistry(bus, node, logger.Named("funcs"))
			}

			return funcapi.WithRegistry(ctx, funcs), nil
		},
		Start: func(ctx context.Context) error {
			if funcs != nil {
				return funcs.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if funcs != nil {
				return funcs.Stop()
			}
			return nil
		},
	})
}
