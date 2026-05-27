// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	sysreg "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

func Supervisor() boot.Component {
	var sup *supervisor.Supervisor

	return boot.New(boot.P{
		Name:      SupervisorName,
		DependsOn: []boot.Name{RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("core")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register lifecycle dependency patterns. "requires" is canonical;
			// "depends_on" is kept so older modules feed the same registry graph.
			for _, pattern := range getLifecycleDependencyPatterns() {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register lifecycle dependency pattern",
						zap.String("path", pattern.Path),
						zap.Error(err))
				}
			}

			// Create dependency resolver that extracts dependencies from registry entries
			depResolver := createDependencyResolver(reg, logger.Named("deps"))

			sup = supervisor.NewSupervisor(bus, logger, supervisor.WithDependencyResolver(depResolver))
			logger.Info("supervisor created with registry dependency resolver")

			// Store supervisor in context for access by other components
			ctx = supervisorapi.WithSupervisor(ctx, sup)

			// Expose service info through supervisor API
			serviceInfo := supervisor.NewServiceInfoAdapter(sup)
			ctx = supervisorapi.WithServiceInfo(ctx, serviceInfo)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if sup != nil {
				return sup.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if sup != nil {
				return sup.Stop()
			}
			return nil
		},
	})
}

func getLifecycleDependencyPatterns() []regapi.DependencyPattern {
	return []regapi.DependencyPattern{
		{Path: "data.lifecycle.requires", Description: "Lifecycle requirements", AllowWildcard: true},
		{Path: "data.lifecycle.depends_on", Description: "Legacy lifecycle dependencies", AllowWildcard: true},
	}
}

// createDependencyResolver creates a supervisor dependency resolver that extracts
// dependencies from registry entries using the registry's topology resolver.
func createDependencyResolver(reg regapi.Registry, _ *zap.Logger) supervisorapi.DependencyResolver {
	regImpl, ok := reg.(*sysreg.Reg)
	if !ok {
		return nil
	}

	resolver := regImpl.DependencyResolver()
	if resolver == nil {
		return nil
	}

	return func(id regapi.ID) ([]regapi.ID, error) {
		entry, err := reg.GetEntry(id)
		if err != nil {
			return nil, nil
		}

		depStrings := resolver.Extract(entry)

		deps := make([]regapi.ID, 0, len(depStrings))
		for _, depStr := range depStrings {
			depID := regapi.ParseID(depStr)
			if depID.NS == "" {
				depID = depID.WithDefaultNS(entry.ID.NS)
			}
			deps = append(deps, depID)
		}

		return deps, nil
	}
}
