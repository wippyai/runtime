// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func (h *DependencyHandler) loadModuleEntries(ctx context.Context, modules []ResolvedModule, snapshot regapi.State, transcoder payload.Transcoder) ([]regapi.Entry, *unpackPlan, error) {
	entries := make([]regapi.Entry, 0)
	plan := &unpackPlan{}
	snapshotByID := entriesByID(snapshot)
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, nil, err
	}

	for _, mod := range modules {
		moduleName := mod.Org + "/" + mod.Name
		deploymentRootModule := h.isDeploymentRoot(moduleName)
		moduleEntries, staged, err := h.loadEntriesForModulePlan(ctx, transcoder, mod)
		if err != nil {
			_ = plan.cleanup()
			return nil, nil, err
		}
		plan.add(staged)
		for i := range moduleEntries {
			// Cold boot marks dependency declarations from the selected root
			// application as deployment roots. Loading that same application
			// through a live Hub update must produce the identical topology;
			// otherwise the update silently turns its application dependencies
			// into transitive module entries and the next update loses their
			// host bindings.
			if deploymentRootModule && moduleEntries[i].Kind == regapi.NamespaceDependency {
				moduleEntries[i].Registry.Root = true
			}
			if keep, ok := preserveHostSnapshotEntry(moduleEntries[i], moduleName, snapshotByID, installedRoots); ok {
				moduleEntries[i] = keep
				continue
			}
			moduleEntries[i] = markModuleEntry(moduleEntries[i], moduleName)
		}
		entries = append(entries, moduleEntries...)
	}

	return entries, plan, nil
}
func entriesByID(entries regapi.State) map[string]regapi.Entry {
	byID := make(map[string]regapi.Entry, len(entries))
	for _, entry := range entries {
		byID[idKey(entry.ID)] = entry
	}
	return byID
}
func rootDependencyModules(ctx context.Context, transcoder payload.Transcoder, entries regapi.State) (map[string]struct{}, error) {
	modules := make(map[string]struct{})
	for _, entry := range entries {
		if !isRootDependency(entry) {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component != "" {
			modules[def.Component] = struct{}{}
		}
	}
	return modules, nil
}
func preserveHostSnapshotEntry(entry regapi.Entry, moduleName string, snapshot map[string]regapi.Entry, installedRoots map[string]struct{}) (regapi.Entry, bool) {
	if _, installed := installedRoots[moduleName]; !installed {
		return regapi.Entry{}, false
	}
	if entryModule(entry) != "" {
		return regapi.Entry{}, false
	}
	existing, ok := snapshot[idKey(entry.ID)]
	if !ok || entryModule(existing) != "" {
		return regapi.Entry{}, false
	}
	return existing, true
}
func (h *DependencyHandler) loadEntriesForModule(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, error) {
	entries, staged, err := h.loadEntriesForModulePlan(ctx, transcoder, mod)
	if staged != nil {
		_ = os.RemoveAll(staged.stagingDir)
	}
	return entries, err
}
func (h *DependencyHandler) loadEntriesForModulePlan(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, *stagedModuleDirectory, error) {
	modulePath, staged, err := h.materializeModuleForLoad(ctx, mod)
	if err != nil {
		return nil, nil, err
	}
	var entries []regapi.Entry
	if mod.Source == moduleSourceReplacementTreeV1 {
		entries, err = loadReplacementEntries(ctx, modulePath, h.logger, transcoder)
	} else {
		entries, err = loadRawEntriesFromPaths(ctx, []string{modulePath}, h.logger, transcoder)
	}
	if err != nil {
		if staged != nil {
			_ = os.RemoveAll(staged.stagingDir)
		}
		return nil, nil, err
	}
	entries, err = h.applyModuleConfigFilters(ctx, modulePath, entries)
	if err != nil {
		if staged != nil {
			_ = os.RemoveAll(staged.stagingDir)
		}
		return nil, nil, err
	}
	if mod.Source == moduleSourceReplacementTreeV1 {
		digest, size, digestErr := digestReplacementTree(modulePath)
		if digestErr != nil {
			return nil, nil, NewDependencyIntegrityError(modKey(mod), digestErr, mod.Digest, mod.SizeBytes)
		}
		if !strings.EqualFold(digest, mod.Digest) || (mod.SizeBytes > 0 && size != mod.SizeBytes) {
			return nil, nil, NewDependencyIntegrityError(modKey(mod), errReplacementChangedWhileLoad, mod.Digest, mod.SizeBytes)
		}
	}
	return entries, staged, nil
}

// applyModuleConfigFilters drops entries the module's wippy.yaml excludes
// (exclude / exclude_meta) when the module is loaded from a directory tree —
// e.g. a lock replacement pointed at the module's source. Without it a host app
// picks up the module's own fixtures (test/_index.yaml under namespace "app"),
// which then collide with the host's real entries during linking. .wapp packs
// are skipped: they were already filtered at publish time.
func (h *DependencyHandler) applyModuleConfigFilters(ctx context.Context, modulePath string, entries []regapi.Entry) ([]regapi.Entry, error) {
	if filepath.Ext(modulePath) == ".wapp" {
		return entries, nil
	}
	cfg, err := depconfig.Load(modulePath)
	if err != nil {
		return entries, nil
	}
	filtered, err := stages.FilterModuleEntries(ctx, cfg, entries)
	if err != nil {
		return nil, NewDependencyLoadError(modulePath, err)
	}
	return filtered, nil
}
func loadRawEntriesFromPaths(
	ctx context.Context,
	paths []string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}

	ldr := loaderFromContext(ctx, logger, transcoder)

	var entries []regapi.Entry
	for _, path := range paths {
		var loaded []regapi.Entry
		if filepath.Ext(path) == ".wapp" {
			var err error
			loaded, err = loadEntriesFromWapp(path)
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
		} else {
			stat, err := os.Stat(path)
			if os.IsNotExist(err) {
				logger.Warn("path not found, skipping", zap.String("path", path))
				continue
			}
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
			if stat.IsDir() {
				dirFS := os.DirFS(path)
				loaded, err = ldr.LoadFS(ctx, dirFS)
				if err != nil {
					return nil, NewDependencyLoadError(path, err)
				}
			} else {
				logger.Warn("unknown path type, skipping", zap.String("path", path))
				continue
			}
		}
		entries = append(entries, loaded...)
	}
	return entries, nil
}
func loaderFromContext(ctx context.Context, logger *zap.Logger, transcoder payload.Transcoder) boot.Loader {
	if ldr := boot.GetLoader(ctx); ldr != nil {
		return ldr
	}

	interpolator := interpolate.NewEntryInterpolator(transcoder,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	return loader.NewLoader(transcoder, logger.Named("loader"), interpolator)
}
func loadEntriesFromWapp(path string) ([]regapi.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return nil, err
	}

	wappEntries, err := reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: we.Kind,
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(unwrapPayloadData(we.Data)),
		}
	}
	return entries, nil
}
func unwrapPayloadData(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	innerData, hasData := m["Data"]
	_, hasFormat := m["Format"]
	if hasData && hasFormat && len(m) == 2 {
		return innerData
	}
	return data
}
