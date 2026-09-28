// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/artifact"
	hubdeps "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/runner"
	regtop "github.com/wippyai/runtime/system/registry/topology"
)

func Registry() boot.Component {
	var histCloser io.Closer

	return boot.New(boot.P{
		Name:      RegistryName,
		DependsOn: []boot.Name{ArtifactName},
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

			hist, closer, err := openHistory(ctx, boot.GetConfig(ctx), logger)
			if err != nil {
				return nil, err
			}
			histCloser = closer
			cfg := boot.GetConfig(ctx)

			// Create state builder
			stateBuilder := regtop.NewStateBuilder(logger, resolver)

			internalKinds := defaultDispatchInternalKinds()
			// Unset means no fixed cap: an operation waits as long as its
			// context allows, because a listener that compiles or analyzes an
			// entry has no meaningful fixed budget. A configured value caps it.
			eventWaitTimeout := time.Duration(0)
			if cfg != nil {
				registryCfg := cfg.Sub(RegistryName)
				if kinds, ok := readKindSlice(registryCfg, RegistryDispatchInternalKinds); ok {
					internalKinds = kinds
				}
				eventWaitTimeout = registryCfg.GetDuration(RegistryEventWaitTimeout, eventWaitTimeout)
			}

			registryOpts := []registry.Option{}

			depHandler, err := newDependencyHandler(ctx, cfg, logger.Named("dependency"), resolver)
			if err != nil {
				logger.Warn("dependency handler disabled", zap.Error(err))
			} else if depHandler != nil {
				// Startup restores installed artifacts locally. Downloads belong
				// to explicit install/update operations, never implicit recovery.
				restoreCtx := regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
				if err := depHandler.PrepareRestore(restoreCtx, hist); err != nil {
					// Startup stops here, so the shutdown hook that owns the history
					// never runs; release it now or the store stays open.
					if histCloser != nil {
						err = errors.Join(err, histCloser.Close())
					}
					return nil, NewDependencyRestoreError(err)
				}
				registryOpts = append(registryOpts,
					registry.WithKindDirective(regapi.NamespaceDependency, regexp.NewDependencyDirective(depHandler.Expand).WithResolutionTransition(depHandler.ReconcileResolution).WithChangesExpansion(depHandler.ExpandChanges)),
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
					runner.WithKindHandlerCheck(func(kind regapi.Kind) bool {
						handlerRegistry := bootpkg.GetHandlerRegistry(ctx)
						if handlerRegistry == nil {
							return true
						}
						return handlerRegistry.HandlesKind(kind)
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

func newDependencyHandler(
	ctx context.Context,
	cfg boot.Config,
	logger *zap.Logger,
	resolver regapi.DependencyResolver,
) (*hubdeps.DependencyHandler, error) {
	var registryCfg boot.Config
	if cfg != nil {
		registryCfg = cfg.Sub(RegistryName)
	}

	opts := hubdeps.DependencyHandlerOptions{
		Logger:   logger,
		Resolver: resolver,
	}
	artifactRegistry := artifact.GetRegistry(ctx)
	if artifactRegistry == nil {
		return nil, ErrArtifactRegistryNotAvailable
	}
	opts.Artifacts = artifactRegistry
	workspaceReplacements, err := lock.WorkspaceReplacements(cfg)
	if err != nil {
		return nil, NewWorkspaceReplacementsError(err)
	}
	opts.WorkspaceReplacements = workspaceReplacements

	if registryCfg != nil {
		opts.ResolveTimeout = registryCfg.GetDuration(RegistryDependencyResolveTimeout, 0)
		opts.DownloadTimeout = registryCfg.GetDuration(RegistryDependencyDownloadTimeout, 0)
		opts.LockPath = registryCfg.GetString(RegistryDependencyLockPath, "")
		opts.VendorDir = registryCfg.GetString(RegistryDependencyVendorDir, "")
	}
	opts.ArtifactRoot = artifact.ConfiguredRoot(cfg, "")

	return hubdeps.NewDependencyHandler(opts)
}
