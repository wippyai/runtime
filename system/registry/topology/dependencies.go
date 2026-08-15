// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"

	"github.com/wippyai/runtime/api/registry"
)

// VisitDependencies walks the canonical topology edges without retaining an
// adjacency list. It is the low-allocation path for validation and auditing;
// callers that need materialized edges should use ResolveDependencies.
func VisitDependencies(state registry.StateMap, resolver registry.DependencyResolver, visit func(source, target registry.ID) error) error {
	var (
		keysBySource  map[registry.ID]entryDepKeys
		needGroups    bool
		needNamespace bool
	)
	for id, entry := range state {
		keys := extractDepKeys(entry, resolver)
		if len(keys.direct)+len(keys.groups)+len(keys.ns) == 0 {
			continue
		}
		if keysBySource == nil {
			keysBySource = make(map[registry.ID]entryDepKeys)
		}
		keysBySource[id] = keys
		needGroups = needGroups || len(keys.groups) != 0
		needNamespace = needNamespace || len(keys.ns) != 0
	}
	if len(keysBySource) == 0 {
		return nil
	}

	var groups map[string][]registry.ID
	var namespaces map[string][]registry.ID
	if needGroups {
		groups = make(map[string][]registry.ID)
	}
	if needNamespace {
		namespaces = make(map[string][]registry.ID)
	}
	if needGroups || needNamespace {
		for id, entry := range state {
			if needGroups {
				for _, group := range entry.Meta.GetSlice(registry.TagGroups) {
					groups[group] = append(groups[group], id)
				}
			}
			if needNamespace && id.NS != "" {
				namespaces[id.NS] = append(namespaces[id.NS], id)
			}
		}
	}

	var (
		seen      map[registry.ID]uint64
		visitMark uint64
	)
	for id, keys := range keysBySource {
		if seen == nil {
			seen = make(map[registry.ID]uint64, len(state))
		}
		visitMark++
		add := func(target registry.ID) error {
			if target.Equal(id) {
				return nil
			}
			if _, exists := state[target]; !exists {
				return nil
			}
			if seen[target] == visitMark {
				return nil
			}
			seen[target] = visitMark
			return visit(id, target)
		}
		for _, target := range keys.direct {
			if err := add(target); err != nil {
				return err
			}
		}
		for _, group := range keys.groups {
			for _, target := range groups[group] {
				if err := add(target); err != nil {
					return err
				}
			}
		}
		for _, namespace := range keys.ns {
			for _, target := range namespaces[namespace] {
				if err := add(target); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ResolveDependencies materializes the canonical topology edges for a state.
// Missing direct targets are ignored, matching the sorter. Group and namespace
// membership is indexed once, so callers do not need to duplicate dependency
// syntax or repeatedly scan the complete registry.
func ResolveDependencies(state registry.StateMap, resolver registry.DependencyResolver) map[registry.ID][]registry.ID {
	result := make(map[registry.ID][]registry.ID, len(state))
	_ = VisitDependencies(state, resolver, func(source, target registry.ID) error {
		result[source] = append(result[source], target)
		return nil
	})
	for id := range result {
		sort.Slice(result[id], func(i, j int) bool { return result[id][i].String() < result[id][j].String() })
	}
	return result
}
