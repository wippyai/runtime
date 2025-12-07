package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
	syscontract "github.com/wippyai/runtime/system/contract"
)

func Contract() boot.Component {
	return boot.New(boot.P{
		Name:      ContractDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}
			node := relay.GetNode(ctx)
			d := syscontract.NewDispatcher(node)
			d.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
