// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
)

func entryModule(entry regapi.Entry) string {
	return entry.Registry.Owner
}

func markModuleEntry(entry regapi.Entry, module string) regapi.Entry {
	entry.Registry.Owner = module
	return entry
}

func moduleIdentities(resolution *regapi.DependencyResolution) (map[string]string, map[string]string) {
	versions := make(map[string]string)
	digests := make(map[string]string)
	if resolution == nil {
		return versions, digests
	}
	for _, module := range resolution.Modules {
		versions[module.Name] = module.Version
		digests[module.Name] = module.Digest
	}
	return versions, digests
}

func (h *DependencyHandler) currentModuleIdentities(ctx context.Context) (map[string]string, map[string]string) {
	if resolution := h.currentResolution(ctx); resolution != nil {
		return moduleIdentities(resolution)
	}
	versions, digests := moduleIdentities(nil)
	if h == nil {
		return versions, digests
	}
	if h.deployment != nil {
		for _, module := range h.deployment.Modules {
			versions[module.Name] = module.Version
			digests[module.Name] = module.Digest
		}
		return versions, digests
	}
	if h.lock == nil {
		return versions, digests
	}
	for _, module := range h.lock.GetModules() {
		if _, exists := versions[module.Name]; !exists {
			versions[module.Name] = module.Version
		}
		if _, exists := digests[module.Name]; !exists {
			digests[module.Name] = module.Hash
		}
	}
	return versions, digests
}

func (h *DependencyHandler) currentResolution(ctx context.Context) *regapi.DependencyResolution {
	if reg := regapi.GetRegistry(ctx); reg != nil {
		return reg.Snapshot().Registry.Resolution
	}
	return nil
}

func (h *DependencyHandler) offlineModules(resolution *regapi.DependencyResolution) []regapi.ResolvedModule {
	capacity := 0
	if resolution != nil {
		capacity += len(resolution.Modules)
	}
	if h != nil && h.lock != nil {
		capacity += len(h.lock.GetModules())
	} else if h != nil && h.deployment != nil {
		capacity += len(h.deployment.Modules)
	}
	modules := make([]regapi.ResolvedModule, 0, capacity)
	if resolution != nil {
		modules = append(modules, resolution.Modules...)
	}
	if h != nil && h.lock != nil {
		for _, locked := range h.lock.GetModules() {
			modules = append(modules, regapi.ResolvedModule{
				Name: locked.Name, Version: locked.Version, VersionID: locked.Version,
				Source: moduleSourceHub, Digest: locked.Hash,
			})
		}
	} else if h != nil && h.deployment != nil {
		modules = append(modules, h.deployment.Modules...)
	}
	return modules
}
