package core

import (
	"context"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/history"
	"github.com/wippyai/runtime/system/registry/runner"
	regtop "github.com/wippyai/runtime/system/registry/topology"
)

func Registry() boot.Component {
	return boot.New(boot.P{
		Name:      RegistryName,
		Phase:     boot.PreInit,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			// Create dependency resolver with default patterns
			resolver := regtop.NewResolver()

			// Register all default patterns
			defaultPatterns := getDefaultDependencyPatterns()
			for _, pattern := range defaultPatterns {
				if err := resolver.RegisterPattern(pattern); err != nil {
					logger.Warn("failed to register default pattern",
						zap.String("path", pattern.Path),
						zap.Error(err))
				}
			}

			// Create registry with resolver
			reg := registry.NewRegistry(
				history.NewMemory(),
				runner.NewBusRunner(bus, logger.Named("runner")),
				regtop.NewStateBuilder(logger, resolver),
				resolver,
				logger.Named("registry"),
			)

			ctx = regapi.WithResolver(ctx, resolver)
			return regapi.WithRegistry(ctx, reg), nil
		},
	})
}

// getDefaultDependencyPatterns returns the core dependency patterns.
// These are generic patterns that don't belong to any specific component.
func getDefaultDependencyPatterns() []regapi.DependencyPattern {
	return []regapi.DependencyPattern{
		{Path: "meta.parent", Description: "Reference to parent component in metadata"},
		{Path: "meta.depends_on", Description: "Explicit dependencies in metadata", AllowWildcard: true},
		{Path: "meta.groups", Description: "Group membership list in metadata", AllowWildcard: true},
		{Path: "data.config", Description: "Reference to a configuration entry"},
		{Path: "data.groups", Description: "Group membership list in data", AllowWildcard: true},
		{Path: "data.imports.*", Description: "Imported components (values only)", AllowWildcard: true},
		{Path: "data.*.depends_on", Description: "Explicit dependencies in nested structures", AllowWildcard: true},
	}
}
