// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
)

// lockedManifestProvider exposes only locally materialized, content-pinned
// modules. It lets the normal resolver validate a graph containing mutable
// replacements without granting that resolver any network capability.
type lockedManifestProvider struct {
	handler *DependencyHandler
	modules map[string]ResolvedModule
}

func newLockedManifestProvider(
	handler *DependencyHandler,
	materializedVersions map[string]string,
	lockedDigests map[string]string,
) ManifestProvider {
	provider := &lockedManifestProvider{
		handler: handler,
		modules: make(map[string]ResolvedModule),
	}
	if handler == nil || handler.lock == nil {
		return provider
	}
	for _, locked := range handler.lock.GetModules() {
		name, err := graph.ParseName(locked.Name)
		if err != nil || locked.Version == "" || materializedVersions[locked.Name] != locked.Version {
			continue
		}
		digest := lockedDigests[locked.Name+"@"+locked.Version]
		if err := validateModuleArtifactIdentity(name, locked.Version, digest); err != nil || digest == "" {
			continue
		}
		provider.modules[locked.Name] = ResolvedModule{
			Org:       name.Organization,
			Name:      name.Module,
			Version:   locked.Version,
			VersionID: locked.Version,
			Source:    moduleSourceHub,
			Digest:    digest,
		}
	}
	return provider
}

func (p *lockedManifestProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	name := org + "/" + module
	mod, ok := p.modules[name]
	if !ok || !storedVersionSatisfies(mod.Version, constraint) {
		return nil, NewDependencyOfflineError("resolve manifest", name)
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}
	entries, err := p.handler.loadEntriesForModule(ctx, transcoder, mod)
	if err != nil {
		return nil, err
	}
	deps, err := manifestDependenciesFromEntries(ctx, transcoder, entries)
	if err != nil {
		return nil, fmt.Errorf("read locked manifest %s@%s: %w", name, mod.Version, err)
	}
	return &ModuleManifest{
		Org:          mod.Org,
		Name:         mod.Name,
		Version:      mod.Version,
		VersionID:    mod.VersionID,
		Digest:       mod.Digest,
		SizeBytes:    mod.SizeBytes,
		Dependencies: deps,
	}, nil
}

func (p *lockedManifestProvider) ListAllVersions(_ context.Context, org, module string) ([]VersionInfo, error) {
	name := org + "/" + module
	mod, ok := p.modules[name]
	if !ok {
		return nil, NewDependencyOfflineError("list versions", name)
	}
	return []VersionInfo{{Version: mod.Version}}, nil
}

func manifestDependenciesFromEntries(
	ctx context.Context,
	transcoder payload.Transcoder,
	entries []regapi.Entry,
) ([]ManifestDep, error) {
	deps := make([]ManifestDep, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		name, err := graph.ParseName(def.Component)
		if err != nil {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "invalid component", def.Component)
		}
		key := name.String() + "@" + def.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deps = append(deps, ManifestDep{
			Org: name.Organization, Name: name.Module, Version: def.Version,
		})
	}
	return deps, nil
}
