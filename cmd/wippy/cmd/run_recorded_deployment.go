// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"go.uber.org/zap"
)

// A lockless host must locate its history before reading packed defaults. The
// registry location belongs to host configuration; the recorded root supplies
// the same packed defaults and profiles as a lock-selected root.
func loadRecordedDeploymentDefaults(ctx context.Context, cfg boot.Config, logger *zap.Logger) (defaults boot.Config, deployment *regapi.Deployment, result error) {
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
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
	sources := moduleapi.NewSourceRegistry()
	ctx = moduleapi.WithSourceRegistry(ctx, sources)
	ctx, err = core.Artifacts().Load(ctx)
	if err != nil {
		return nil, nil, err
	}
	handler, err := core.NewDependencyHandler(ctx, cfg, logger.Named("dependencies"), nil)
	if err != nil {
		return nil, nil, err
	}
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
