// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"encoding/hex"
	"sort"
	"strings"

	"github.com/wippyai/runtime/boot/deps/graph"
)

// lockedResolution verifies the module selection recorded by wippy update.
func (h *DependencyHandler) lockedResolution(
	deps []DependencyDefinition,
	materializedVersions map[string]string,
) ([]ResolvedModule, bool) {
	if h == nil || h.lock == nil || len(h.replacements) != 0 {
		return nil, false
	}

	locked := h.lock.GetModules()
	if len(locked) == 0 {
		return nil, false
	}

	selected := make(map[string]ResolvedModule, len(locked))
	for _, mod := range locked {
		if mod.Name == "" || mod.Version == "" || mod.Hash == "" {
			return nil, false
		}
		if materializedVersions[mod.Name] != mod.Version {
			return nil, false
		}

		name, err := graph.ParseName(mod.Name)
		if err != nil {
			return nil, false
		}
		algorithm, digest, err := parseExpectedDigest(mod.Hash)
		if err != nil || algorithm != "sha256" || len(digest) != 64 {
			return nil, false
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, false
		}
		if _, duplicate := selected[mod.Name]; duplicate {
			return nil, false
		}

		selected[mod.Name] = ResolvedModule{
			Org:       name.Organization,
			Name:      name.Module,
			Version:   mod.Version,
			VersionID: mod.Version,
			Source:    moduleSourceHub,
			Digest:    "sha256:" + strings.ToLower(digest),
		}
	}

	for _, dep := range deps {
		mod, ok := selected[dep.Component]
		if !ok || !storedVersionSatisfies(mod.Version, dep.Version) {
			return nil, false
		}
	}

	resolved := make([]ResolvedModule, 0, len(selected))
	for _, mod := range selected {
		resolved = append(resolved, mod)
	}
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].Org != resolved[j].Org {
			return resolved[i].Org < resolved[j].Org
		}
		return resolved[i].Name < resolved[j].Name
	})
	return resolved, true
}
