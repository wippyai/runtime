// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"go.uber.org/zap"
)

// A lockless host must locate its history before reading packed defaults. The
// registry location belongs to host configuration; the recorded root supplies
// the same packed defaults and profiles as a lock-selected root.
func loadRecordedDeploymentDefaults(ctx context.Context, cfg boot.Config, requestedRoot, registryURL string, logger *zap.Logger) (defaults boot.Config, deployment *regapi.Deployment, result error) {
	cfg, err := bootconfig.ResolveVariablesIn(cfg, "registry")
	if err != nil {
		return nil, nil, err
	}
	history, closer, err := core.OpenRegistryHistory(cfg, logger.Named("history"))
	if err != nil {
		return nil, nil, err
	}
	if closer != nil {
		defer func() { result = errors.Join(result, closer.Close()) }()
	}
	resolutions, ok := history.(regapi.ResolutionHistory)
	if !ok {
		return nil, nil, nil
	}
	head, err := history.Head()
	if err != nil {
		var categorized apierror.Error
		if errors.As(err, &categorized) && categorized.Kind() == apierror.NotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	resolution, err := resolutions.GetDependencyResolution(head)
	if errors.Is(err, regapi.ErrDependencyResolutionNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !resolution.Valid() || resolution.Deployment == nil {
		return nil, nil, fmt.Errorf("recorded dependency resolution has no valid deployment baseline")
	}
	deployment = resolution.Deployment.Canonical()
	if requestedRoot != "" {
		if err := validateHubDeploymentReference(requestedRoot, deployment.Root, recordedRootVersion(deployment), "recorded deployment"); err != nil {
			return nil, nil, err
		}
	}
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
	sources := moduleapi.NewSourceRegistry()
	ctx = moduleapi.WithSourceRegistry(ctx, sources)
	ctx, err = core.Artifacts().Load(ctx)
	if err != nil {
		return nil, nil, err
	}
	var client hub.HubClient
	if registryURL != "" {
		client, err = hub.NewClientFromAuth(registryURL)
		if err != nil {
			return nil, nil, err
		}
	}
	handler, err := core.NewDependencyHandler(ctx, cfg, logger.Named("dependencies"), nil, client)
	if err != nil {
		return nil, nil, err
	}
	defer handler.Close()
	if err := handler.PrepareRestore(regapi.WithDependencyAccess(ctx, regapi.DependencyAccessOnline), history); err != nil {
		return nil, nil, err
	}
	source, ok := sources.Snapshot()[deployment.Root]
	if !ok {
		return nil, nil, fmt.Errorf("recorded deployment root %s is unavailable", deployment.Root)
	}
	defaults, err = loadPackRuntimeDefaults(source.LoadPath, logger)
	return defaults, deployment, err
}

func recordedRootVersion(deployment *regapi.Deployment) string {
	for _, module := range deployment.Modules {
		if module.Name == deployment.Root {
			return module.Version
		}
	}
	return ""
}

// Packed defaults are available only after locating the recorded deployment.
// They may configure the application, but must not send this same invocation
// to a different history store when its runtime is composed.
func validateRecordedHistoryConfig(hostCfg, runtimeCfg boot.Config) error {
	hostCfg, err := bootconfig.ResolveVariablesIn(hostCfg, "registry")
	if err != nil {
		return err
	}
	host, effective := hostCfg.Sub("registry"), runtimeCfg.Sub("registry")
	changed := func() error {
		return fmt.Errorf("packed configuration changes the recorded deployment history store; configure its location in the host config or a local profile")
	}
	if host.GetBool(core.RegistryEnableHistory, true) != effective.GetBool(core.RegistryEnableHistory, true) ||
		host.GetString(core.RegistryHistoryType, "memory") != effective.GetString(core.RegistryHistoryType, "memory") {
		return changed()
	}
	switch host.GetString(core.RegistryHistoryType, "memory") {
	case "sqlite":
		hostPath, err := filepath.Abs(host.GetString(core.RegistryHistoryPath, ".wippy/registry.db"))
		if err != nil {
			return err
		}
		effectivePath, err := filepath.Abs(effective.GetString(core.RegistryHistoryPath, ".wippy/registry.db"))
		if err != nil {
			return err
		}
		if hostPath != effectivePath {
			return changed()
		}
	case "postgres":
		for _, key := range []string{core.RegistryHistoryDSN, core.RegistryHistorySchema} {
			if host.GetString(key, "") != effective.GetString(key, "") {
				return changed()
			}
		}
	}
	return nil
}
