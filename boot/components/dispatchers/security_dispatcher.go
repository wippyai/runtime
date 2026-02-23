// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/system/security"
)

func Security() boot.Component {
	var svc *security.Dispatcher

	return boot.New(boot.P{
		Name:      SecurityDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}
			svc = security.NewDispatcher(4)
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if svc != nil {
				return svc.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if svc != nil {
				return svc.Stop(ctx)
			}
			return nil
		},
	})
}
