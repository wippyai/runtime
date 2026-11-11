// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

// XMLConfig configures the XML dispatcher.
type XMLConfig struct {
	Workers int
}

// XML creates the dispatcher component for XML stream commands.
func XML(cfg ...XMLConfig) boot.Component {
	var svc *xmlcmd.Dispatcher
	var config XMLConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}

	return boot.New(boot.P{
		Name:      XMLDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			var opts []xmlcmd.Option
			if config.Workers > 0 {
				opts = append(opts, xmlcmd.WithWorkers(config.Workers))
			}

			svc = xmlcmd.NewDispatcher(opts...)
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
