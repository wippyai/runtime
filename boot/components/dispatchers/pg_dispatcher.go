// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	pg "github.com/wippyai/runtime/service/pg"
)

func PGDispatcher() boot.Component {
	return boot.New(boot.P{
		Name:      PGDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			d := pg.NewDispatcher()
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
