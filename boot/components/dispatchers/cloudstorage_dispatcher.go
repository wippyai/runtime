// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/cloudstorage"
)

func CloudStorage() boot.Component {
	return boot.New(boot.P{
		Name:      CloudStorageDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}
			svc := cloudstorage.NewDispatcher()
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
