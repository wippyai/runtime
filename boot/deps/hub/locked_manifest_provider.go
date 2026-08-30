// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
)

// lockedManifestProvider exposes only locally materialized, content-pinned
// modules. It lets the normal resolver validate a graph containing mutable
// replacements without granting that resolver any network capability.
type lockedManifestProvider struct {
	handler *DependencyHandler
	modules map[string]map[string]ResolvedModule
}

func newLockedManifestProvider(
	handler *DependencyHandler,
	selectedModules []regapi.ResolvedModule,
) ManifestProvider {
	provider := &lockedManifestProvider{
		handler: handler,
		modules: make(map[string]map[string]ResolvedModule),
	}
	if handler == nil {
		return provider
	}
	for _, selected := range selectedModules {
		name, err := graph.ParseName(selected.Name)
		if err != nil || selected.Version == "" {
			continue
		}
		if err := validateStoredModuleArtifactIdentity(name, selected.Version, selected.Source, selected.Digest); err != nil {
			continue
		}
		versions := provider.modules[selected.Name]
		if versions == nil {
			versions = make(map[string]ResolvedModule)
			provider.modules[selected.Name] = versions
		}
		versions[strings.TrimPrefix(selected.Version, "v")] = ResolvedModule{
			Org:       name.Organization,
			Name:      name.Module,
			Version:   selected.Version,
			VersionID: selected.VersionID,
			Source:    selected.Source,
			Digest:    selected.Digest,
			SizeBytes: selected.SizeBytes,
			Protected: selected.Protected,
		}
	}
	return provider
}

func (p *lockedManifestProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	name := org + "/" + module
	versions := p.modules[name]
	mod, ok := versions[strings.TrimPrefix(constraint, "v")]
	if !ok {
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
	modules := p.modules[name]
	if len(modules) == 0 {
		return nil, NewDependencyOfflineError("list versions", name)
	}
	versions := make([]VersionInfo, 0, len(modules))
	for _, mod := range modules {
		versions = append(versions, VersionInfo{Version: mod.Version})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })
	return versions, nil
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
