// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	bootloader "github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/runtime/cmd/internal/hubclient"
	"go.uber.org/zap"
)

// prepareRunDependencies repairs a local application's stale lock before any
// runtime service starts. A complete lock remains an offline authority; only a
// source dependency that the lock cannot satisfy permits Hub resolution.
func prepareRunDependencies(
	ctx context.Context,
	cfg boot.Config,
	registryURL string,
	logger *zap.Logger,
) error {
	lockPath, err := lock.Find(".", defaultLockFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return NewLoadLockFileError(err)
	}

	lockObj, err := newConfiguredLock(lockPath, cfg, logger)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}
	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	// A published deployment is rooted by the lock itself. Its exact graph is
	// materialized later by LoadFromLockFile and is never inferred from the
	// caller's working directory.
	if len(lockObj.GetRootModules()) != 0 {
		return nil
	}
	ldr := boot.GetLoader(ctx)
	if ldr == nil {
		transcoder := payload.GetTranscoder(ctx)
		if transcoder == nil {
			return ErrTranscoderNotFound
		}
		interpolator := interpolate.NewEntryInterpolator(transcoder,
			interpolate.WithInterpolator(interpolate.LoadFile),
		)
		ldr = bootloader.NewLoader(transcoder, logger.Named("loader"), interpolator)
	}

	dirs := lockObj.GetDirectories()
	loaded, err := loadDependencyScanEntries(ctx, ldr, dirs.Src, lockObj, logger)
	if err != nil {
		return NewLoadEntriesFromSourceError(err)
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}
	roots := extractRootDependencies(loaded, transcoder)
	if lockSatisfiesSource(lockObj, roots) {
		return nil
	}

	client, err := hubclient.New(hubclient.Options{RegistryURL: registryURL})
	if err != nil {
		return NewCreateHubClientError(err)
	}
	resolved, err := resolveRunDependencies(ctx, client, lockObj, roots)
	if err != nil {
		return err
	}

	modules := make([]lock.Module, 0, len(resolved.Modules))
	selected := make(map[string]struct{}, len(resolved.Modules))
	for _, module := range resolved.Modules {
		name := module.Org + "/" + module.Name
		selected[name] = struct{}{}
		modules = append(modules, lock.Module{
			Name:    name,
			Version: module.Version,
			Hash:    module.Digest,
		})
	}
	// Replacements are intentionally absent from Hub resolution. Retain their
	// selected rows so source ownership and deployment-root identity do not
	// disappear while repairing unrelated dependencies.
	for _, module := range lockObj.GetModules() {
		if _, ok := selected[module.Name]; ok {
			continue
		}
		if _, replaced := lockObj.GetReplacement(module.Name); replaced {
			modules = append(modules, module)
		}
	}
	lockObj.ReplaceModules(modules)

	// Download and verify the candidate graph before publishing it. Unpacked
	// directories are refreshed only after the lock is committed, by the normal
	// lock loader, so a failed preparation cannot replace the active sources.
	options := lockObj.GetOptions()
	lockObj.SetOptions(lock.Options{})
	err = entries.EnsureModulesInstalledFromLock(ctx, lockObj, logger.Named("modules"), client)
	lockObj.SetOptions(options)
	if err != nil {
		return NewEnsureModulesInstalledError(err)
	}
	if err := lockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	logger.Info("completed stale dependency lock", zap.Int("modules", len(modules)))
	return nil
}

func lockSatisfiesSource(lockObj *lock.Lock, roots []dependencyRequest) bool {
	if lockObj == nil {
		return false
	}
	for _, root := range roots {
		name := root.Org + "/" + root.Module
		module, ok := lockObj.GetModule(name)
		if _, replaced := lockObj.GetReplacement(name); replaced && !ok {
			constraint := strings.TrimSpace(root.Constraint)
			if constraint == "" || constraint == "*" || strings.HasPrefix(constraint, "@") {
				continue
			}
		}
		if !ok || !lockedVersionSatisfies(module.Version, root.Constraint) {
			return false
		}
		if _, replaced := lockObj.GetReplacement(name); !replaced && module.Hash == "" {
			return false
		}
	}
	return true
}

func lockedVersionSatisfies(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if version == "" {
		return false
	}
	if constraint == "" || constraint == "*" || strings.HasPrefix(constraint, "@") {
		return true
	}
	v, err := semver.ParseVersion(version)
	if err != nil {
		return false
	}
	c, err := semver.ParseConstraint(constraint)
	return err == nil && c.Match(v)
}

func resolveRunDependencies(
	ctx context.Context,
	provider hub.HubClient,
	lockObj *lock.Lock,
	roots []dependencyRequest,
) (*hub.ResolveDependenciesResult, error) {
	definitions := make([]hub.DependencyDefinition, 0, len(roots))
	for _, root := range roots {
		definitions = append(definitions, hub.DependencyDefinition{
			Component: root.Org + "/" + root.Module,
			Version:   root.Constraint,
		})
	}
	handler, err := hub.NewDependencyHandler(hub.DependencyHandlerOptions{
		Hub:                   provider,
		LockPath:              lockObj.Path(),
		WorkspaceReplacements: lockObj.GetReplacements(),
	})
	if err != nil {
		return nil, NewBuildDependencyGraphError(err)
	}
	modules, err := handler.ResolveWorkspaceDependencies(ctx, definitions)
	if err != nil {
		return nil, err
	}
	return &hub.ResolveDependenciesResult{Modules: modules}, nil
}
