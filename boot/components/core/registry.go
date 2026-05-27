// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	hubdeps "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historynil "github.com/wippyai/runtime/system/registry/history/nil"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/runner"
	regtop "github.com/wippyai/runtime/system/registry/topology"
)

func Registry() boot.Component {
	var histCloser io.Closer

	return boot.New(boot.P{
		Name:      RegistryName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("registry")
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

			// Determine history implementation based on config
			var hist regapi.History
			cfg := boot.GetConfig(ctx)
			if cfg != nil {
				registryCfg := cfg.Sub(RegistryName)
				enableHistory := registryCfg.GetBool(RegistryEnableHistory, true)

				if !enableHistory {
					hist = historynil.New()
				} else {
					historyType := registryCfg.GetString(RegistryHistoryType, "memory")

					switch historyType {
					case "sqlite":
						historyPath := registryCfg.GetString(RegistryHistoryPath, ".wippy/registry.db")
						absPath, err := filepath.Abs(historyPath)
						if err != nil {
							return nil, NewHistoryPathError(err)
						}

						sqliteHist, err := sqlite.NewSQLite(absPath, logger.Named("history"))
						if err != nil {
							return nil, NewSQLiteHistoryError(err)
						}
						hist = sqliteHist
						histCloser = sqliteHist

					case "nil":
						hist = historynil.New()

					case "memory":
						hist = historymem.New()

					default:
						logger.Warn("unknown history type, defaulting to memory", zap.String("type", historyType))
						hist = historymem.New()
					}
				}
			} else {
				hist = historymem.New()
			}

			// Create state builder
			stateBuilder := regtop.NewStateBuilder(logger, resolver)

			internalKinds := defaultDispatchInternalKinds()
			eventWaitTimeout := event.DefaultAwaitTimeout
			if cfg != nil {
				registryCfg := cfg.Sub(RegistryName)
				if kinds, ok := readKindSlice(registryCfg, RegistryDispatchInternalKinds); ok {
					internalKinds = kinds
				}
				eventWaitTimeout = registryCfg.GetDuration(RegistryEventWaitTimeout, eventWaitTimeout)
			}

			registryOpts := []registry.Option{}

			depHandler, err := newDependencyHandler(cfg, logger.Named("dependency"))
			if err != nil {
				logger.Warn("dependency handler disabled", zap.Error(err))
			} else if depHandler != nil {
				registryOpts = append(registryOpts,
					registry.WithKindDirective(regapi.NamespaceDependency, regexp.NewDependencyDirective(depHandler.Expand)),
				)
			}

			// Create registry with resolver
			reg := registry.NewRegistry(
				hist,
				runner.NewBusRunner(bus, logger.Named("runner"), stateBuilder,
					runner.WithDispatchPolicy(runner.NewKindDispatchPolicy(internalKinds)),
					runner.WithEventWaitTimeout(eventWaitTimeout),
					runner.WithTransactionParticipants(func() []string {
						handlerRegistry := bootpkg.GetHandlerRegistry(ctx)
						if handlerRegistry == nil {
							return nil
						}
						return handlerRegistry.TransactionParticipants()
					}),
				),
				stateBuilder,
				resolver,
				logger.Named("registry"),
				registryOpts...,
			)

			ctx = regapi.WithResolver(ctx, resolver)
			ctx = regapi.WithRegistry(ctx, reg)

			return ctx, nil
		},
		Stop: func(_ context.Context) error {
			if histCloser != nil {
				return histCloser.Close()
			}
			return nil
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

func defaultDispatchInternalKinds() []regapi.Kind {
	return []regapi.Kind{
		regapi.EntryKind,
		regapi.NamespaceDependency,
		regapi.NamespaceRequirement,
		regapi.NamespaceDefinition,
	}
}

func readKindSlice(cfg boot.Config, key boot.Name) ([]regapi.Kind, bool) {
	raw, ok := cfg.Get(key)
	if !ok {
		return nil, false
	}

	var values []string
	switch v := raw.(type) {
	case []string:
		values = v
	case []any:
		values = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case string:
		values = []string{v}
	default:
		return nil, false
	}

	kinds := make([]regapi.Kind, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		kinds = append(kinds, trimmed)
	}
	return kinds, true
}

func newDependencyHandler(cfg boot.Config, logger *zap.Logger) (*hubdeps.DependencyHandler, error) {
	var registryCfg boot.Config
	if cfg != nil {
		registryCfg = cfg.Sub(RegistryName)
	}

	opts := hubdeps.DependencyHandlerOptions{
		Logger: logger,
	}

	if registryCfg != nil {
		opts.ResolveTimeout = registryCfg.GetDuration(RegistryDependencyResolveTimeout, 0)
		opts.DownloadTimeout = registryCfg.GetDuration(RegistryDependencyDownloadTimeout, 0)
		opts.LockPath = registryCfg.GetString(RegistryDependencyLockPath, "")
		opts.VendorDir = registryCfg.GetString(RegistryDependencyVendorDir, "")
	}

	return hubdeps.NewDependencyHandler(opts)
}
