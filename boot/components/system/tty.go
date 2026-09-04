// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	processapi "github.com/wippyai/runtime/api/process"
	ttyapi "github.com/wippyai/runtime/api/tty"
	ttysystem "github.com/wippyai/runtime/system/tty"
)

const FrameResolverOrderTTY = 210

// TTY installs the transport-neutral terminal broker and its process-option
// resolver. The boot graph, rather than package initialization, owns both.
func TTY() boot.Component {
	var service *ttysystem.Service
	return boot.New(boot.P{
		Name:      TTYName,
		DependsOn: []boot.Name{FrameResolversName, LifecycleName},
		Load: func(ctx context.Context) (context.Context, error) {
			resolvers := ctxapi.FrameResolversFrom(ctx)
			if resolvers == nil {
				return nil, ErrFrameResolversMissing
			}
			service = ttysystem.NewService()
			ctx = ttyapi.WithService(ctx, service)
			if ttyapi.GetService(ctx) != service {
				_ = service.Close()
				return nil, ErrTTYServiceAlreadyInstalled
			}
			lifecycle := processapi.GetLifecycleRegistry(ctx)
			if lifecycle == nil {
				_ = service.Close()
				return nil, ErrLifecycleRegistryNotAvailable
			}
			lifecycle.Register("tty", service)
			if err := resolvers.Register("tty", FrameResolverOrderTTY, ttyapi.ResolveTerminalOption); err != nil {
				_ = service.Close()
				return nil, err
			}
			return ctx, nil
		},
		Stop: func(ctx context.Context) error {
			if lifecycle := processapi.GetLifecycleRegistry(ctx); lifecycle != nil {
				lifecycle.Unregister("tty")
			}
			if service == nil {
				return nil
			}
			return service.Close()
		},
	})
}
