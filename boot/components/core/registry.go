// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/backoff"

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
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historynil "github.com/wippyai/runtime/system/registry/history/nil"
	"github.com/wippyai/runtime/system/registry/history/postgres"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/runner"
	regtop "github.com/wippyai/runtime/system/registry/topology"
)

func Registry() boot.Component {
	var histCloser io.Closer
	var publicationCancel context.CancelFunc
	var publicationDone chan struct{}

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

			// Determine history implementation based on config
			var hist regapi.History
			cfg := boot.GetConfig(ctx)
			if cfg != nil {
				registryCfg := cfg.Sub(RegistryName)
				enableHistory := registryCfg.GetBool(RegistryEnableHistory, true)

				if !enableHistory {
					hist = historynil.New()
				} else {
					historyType := registryCfg.GetString(RegistryHistoryType, "")
					if historyType == "" {
						historyType = "memory"
						if registryCfg.GetString("history_endpoint", "") != "" || registryCfg.GetString("history_registry_id", "") != "" {
							historyType = "grpc"
						}
					}

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

					case "grpc":
						dialConfig, err := historyConnectionConfig(ctx, registryCfg)
						if err != nil {
							return nil, err
						}
						remoteHist, err := remote.Dial(ctx, dialConfig)
						if err != nil {
							return nil, err
						}
						hist = remoteHist
						histCloser = remoteHist

					case "postgres":
						historyDSN := registryCfg.GetString(RegistryHistoryDSN, "")
						historySchema := registryCfg.GetString(RegistryHistorySchema, "")

						postgresHist, err := postgres.NewPostgres(historyDSN, historySchema, logger.Named("history"))
						if err != nil {
							return nil, NewPostgresHistoryError(err)
						}
						hist = postgresHist
						histCloser = postgresHist

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

			depHandler, err := newDependencyHandler(ctx, cfg, logger.Named("dependency"), resolver)
			if err != nil {
				logger.Warn("dependency handler disabled", zap.Error(err))
			} else if depHandler != nil {
				if err := depHandler.PrepareRestore(ctx, hist); err != nil {
					if histCloser != nil {
						_ = histCloser.Close()
					}
					return nil, fmt.Errorf("prepare dependency restore: %w", err)
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
		Start: func(ctx context.Context) error {
			reg, ok := regapi.GetRegistry(ctx).(*registry.Reg)
			if !ok {
				return nil
			}
			if _, ok := reg.History().(regapi.PublishedHistory); !ok {
				return nil
			}
			publicationCtx, cancel := context.WithCancel(ctx)
			publicationCancel = cancel
			publicationDone = make(chan struct{})
			go func() {
				defer close(publicationDone)
				for {
					err := reg.FollowPublications(publicationCtx)
					if publicationCtx.Err() != nil || errors.Is(err, context.Canceled) {
						return
					}
					logapi.GetLogger(ctx).Error("history publication follow failed", zap.Error(err))
					timer := time.NewTimer(backoff.DefaultConfig.BaseDelay)
					select {
					case <-publicationCtx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}()
			return nil
		},
		Stop: func(ctx context.Context) error {
			var stopErr error
			if publicationCancel != nil {
				publicationCancel()
				select {
				case <-publicationDone:
				case <-ctx.Done():
					stopErr = ctx.Err()
				}
			}
			if histCloser != nil {
				return errors.Join(stopErr, histCloser.Close())
			}
			return stopErr
		},
	})
}

// getDefaultDependencyPatterns returns the core dependency patterns.
// These are generic patterns that don't belong to any specific component.
func getDefaultDependencyPatterns() []regapi.DependencyPattern {
	return regtop.RegistryDependencyPatterns()
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
		return nil, fmt.Errorf("artifact registry is not initialized")
	}
	opts.Artifacts = artifactRegistry
	workspaceReplacements, err := lock.WorkspaceReplacements(cfg)
	if err != nil {
		return nil, fmt.Errorf("load workspace replacements: %w", err)
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
