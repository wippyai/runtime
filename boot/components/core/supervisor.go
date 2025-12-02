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
		DependsOn: []boot.ComponentName{RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
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

			// Register lifecycle dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:          "data.lifecycle.depends_on",
				Description:   "Lifecycle dependencies",
				AllowWildcard: true,
			}); err != nil {
				logger.Warn("failed to register lifecycle dependency pattern", zap.Error(err))
			}

			// Create dependency resolver that extracts dependencies from registry entries
			depResolver := createDependencyResolver(reg, logger.Named("deps"))

			sup = supervisor.NewSupervisor(bus, logger.Named("core"), supervisor.WithDependencyResolver(depResolver))
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

// createDependencyResolver creates a supervisor dependency resolver that extracts
// dependencies from registry entries using the registry's topology resolver.
func createDependencyResolver(reg regapi.Registry, logger *zap.Logger) supervisorapi.DependencyResolver {
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
			deps = append(deps, regapi.ParseID(depStr))
		}

		return deps, nil
	}
}
