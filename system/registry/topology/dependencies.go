// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"

	"github.com/wippyai/runtime/api/registry"
)

// ResolveDependencies materializes the canonical topology edges for a state.
// Missing direct targets are ignored, matching the sorter. Group and namespace
// membership is indexed once, so callers do not need to duplicate dependency
// syntax or repeatedly scan the complete registry.
func ResolveDependencies(state registry.StateMap, resolver registry.DependencyResolver) map[registry.ID][]registry.ID {
	groups := make(map[string][]registry.ID)
	namespaces := make(map[string][]registry.ID)
	for id, entry := range state {
		for _, group := range entry.Meta.GetSlice(registry.TagGroups) {
			groups[group] = append(groups[group], id)
		}
		if id.NS != "" {
			namespaces[id.NS] = append(namespaces[id.NS], id)
		}
	}

	result := make(map[registry.ID][]registry.ID, len(state))
	for id, entry := range state {
		keys := extractDepKeys(entry, resolver)
		seen := make(map[registry.ID]struct{}, len(keys.direct))
		add := func(target registry.ID) {
			if target.Equal(id) {
				return
			}
			if _, exists := state[target]; !exists {
				return
			}
			if _, exists := seen[target]; exists {
				return
			}
			seen[target] = struct{}{}
			result[id] = append(result[id], target)
		}
		for _, target := range keys.direct {
			add(target)
		}
		for _, group := range keys.groups {
			for _, target := range groups[group] {
				add(target)
			}
		}
		for _, namespace := range keys.ns {
			for _, target := range namespaces[namespace] {
				add(target)
			}
		}
		sort.Slice(result[id], func(i, j int) bool {
			return result[id][i].String() < result[id][j].String()
		})
	}
	return result
}
