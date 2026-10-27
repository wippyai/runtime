// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

type residentManifestProvider struct {
	ManifestProvider
	modules  map[string]regapi.ResolvedModule
	entries  map[string]regapi.State
	fallback bool
}

func newResidentManifestProvider(base ManifestProvider, selections []regapi.ResolvedModule, state regapi.State) ManifestProvider {
	provider := &residentManifestProvider{ManifestProvider: base,
		modules: make(map[string]regapi.ResolvedModule), entries: make(map[string]regapi.State)}
	for _, entry := range state {
		if owner := entryModule(entry); owner != "" {
			provider.entries[owner] = append(provider.entries[owner], entry)
		}
	}
	for _, module := range selections {
		if _, recorded := provider.modules[module.Name]; recorded || module.Digest == "" || len(provider.entries[module.Name]) == 0 {
			continue
		}
		provider.modules[module.Name] = module
	}
	return provider
}

// newResidentFallbackProvider serves resident declarations only for a release
// the registry does not hold. A registry that serves the release keeps the
// last word, so a different digest still fails the lock pin.
func newResidentFallbackProvider(base ManifestProvider, selections []regapi.ResolvedModule, state regapi.State) ManifestProvider {
	provider := newResidentManifestProvider(base, selections, state).(*residentManifestProvider)
	provider.fallback = true
	return provider
}

// lockPinnedModules keeps the recorded modules whose release and digest equal
// a lock pin.
func lockPinnedModules(modules []regapi.ResolvedModule, pins map[string]string) []regapi.ResolvedModule {
	var pinned []regapi.ResolvedModule
	for _, module := range modules {
		if pin := pins[module.Name+"@"+module.Version]; pin != "" && module.Digest != "" && artifactDigestsEqual(module.Digest, pin) {
			pinned = append(pinned, module)
		}
	}
	return pinned
}

func (p *residentManifestProvider) GetManifest(ctx context.Context, org, name, version string) (*ModuleManifest, error) {
	module, present := p.modules[org+"/"+name]
	if !present || strings.TrimPrefix(module.Version, "v") != strings.TrimPrefix(version, "v") {
		return p.ManifestProvider.GetManifest(ctx, org, name, version)
	}
	if p.fallback {
		manifest, err := p.ManifestProvider.GetManifest(ctx, org, name, version)
		if !errors.Is(err, ErrModuleNotFound) {
			return manifest, err
		}
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}
	dependencies, err := manifestDependenciesFromEntries(ctx, transcoder, p.entries[module.Name])
	if err != nil {
		return nil, NewDependencyLoadError(module.Name+"@"+module.Version, err)
	}
	return &ModuleManifest{Org: org, Name: name, Version: module.Version, VersionID: module.VersionID,
		Digest: module.Digest, SizeBytes: module.SizeBytes, Protected: module.Protected, Dependencies: dependencies}, nil
}
