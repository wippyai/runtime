// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
)

func entryModule(entry regapi.Entry) string {
	return entry.Registry.Owner
}

// residentUnchangedModuleEntries returns the live entries of selected modules
// whose artifact and parameters are unchanged across a version transition.
// Modules installed at runtime are materialized by dependency expansion and
// never appear in authored history, so a replayed target state carries no
// entries for them; their resident materialization is the target's. Entries
// the target already supplies take precedence.
func residentUnchangedModuleEntries(
	current regapi.State,
	target []regapi.Entry,
	controlled map[string]struct{},
	desired map[string]struct{},
	touched map[string]struct{},
) []regapi.Entry {
	present := make(map[string]struct{}, len(target))
	for _, entry := range target {
		present[idKey(entry.ID)] = struct{}{}
	}
	var resident []regapi.Entry
	for _, entry := range current {
		module := entryModule(entry)
		if module == "" {
			continue
		}
		if _, owned := controlled[module]; !owned {
			continue
		}
		if _, selected := desired[module]; !selected {
			continue
		}
		if _, changed := touched[module]; changed {
			continue
		}
		if _, supplied := present[idKey(entry.ID)]; supplied {
			continue
		}
		resident = append(resident, entry)
	}
	return resident
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
	// Committed selections are authoritative; the locked provider uses later
	// baseline records only for releases absent from the committed graph.
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
