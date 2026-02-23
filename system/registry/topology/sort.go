// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/graph"
)

const (
	// Prefixes for dependency types
	groupPrefix = "group:"
	nsPrefix    = "ns:"
)

// parseDependency parses a dependency string which can be:
// - Direct reference: "service.database" (uses source namespace) or "other-ns:service.database"
// - Group reference: "group:backend-services"
// - Namespace reference: "ns:backend"
func parseDependency(dep string) (depType string, value string) {
	if strings.HasPrefix(dep, groupPrefix) {
		return "group", strings.TrimPrefix(dep, groupPrefix)
	}
	if strings.HasPrefix(dep, nsPrefix) {
		return "namespace", strings.TrimPrefix(dep, nsPrefix)
	}

	return "direct", dep
}

// resolveDependencyID resolves a direct dependency string to an Process, considering the source namespace
func resolveDependencyID(sourceNS string, depStr string) registry.ID {
	// If the dependency already has a namespace, use it as is
	if strings.Contains(depStr, ":") {
		return registry.ParseID(depStr)
	}
	// Otherwise, inherit the source namespace
	return registry.NewID(sourceNS, depStr)
}

// SortEntriesByDependency sorts entries based on dependencies.
// Dependencies can be specified in depends_on using:
// - Direct references: "service.database" (uses source namespace) or "other-ns:service.database"
// - Group references: "group:backend-services"
// - Namespace references: "ns:backend"
func SortEntriesByDependency(entries []registry.Entry, resolver registry.DependencyResolver) ([]registry.Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Build dependency graph and mappings
	g := graph.New[registry.ID, any]()
	entryMap := make(map[registry.ID]registry.Entry, len(entries))
	groupMap := make(map[string][]registry.ID)
	nsMap := make(map[string][]registry.ID)

	// First pass: build all mappings
	for _, entry := range entries {
		g.AddNode(entry.ID)
		entryMap[entry.ID] = entry

		// Build group mapping from explicit groups
		explicitGroups := entry.Meta.GetSlice(registry.TagGroups)
		for _, group := range explicitGroups {
			groupMap[group] = append(groupMap[group], entry.ID)
		}

		// Build namespace mapping
		if entry.ID.NS != "" {
			nsMap[entry.ID.NS] = append(nsMap[entry.ID.NS], entry.ID)
		}
	}

	// Second pass: process all dependencies
	for _, entry := range entries {
		dependencies := entry.Meta.GetSlice(registry.TagDependsOn)

		if resolver != nil {
			dependencies = append(dependencies, resolver.Extract(entry)...)
		}

		for _, dep := range dependencies {
			depType, value := parseDependency(dep)

			switch depType {
			case "direct":
				// Handle direct dependency, respecting source namespace
				targetID := resolveDependencyID(entry.ID.NS, value)
				if _, exists := entryMap[targetID]; exists {
					g.AddEdge(targetID, entry.ID, 1, nil)
				}

			case "group":
				// Handle group dependency
				if members, exists := groupMap[value]; exists {
					for _, memberID := range members {
						if !memberID.Equal(entry.ID) { // Avoid self-dependency
							g.AddEdge(memberID, entry.ID, 1, nil)
						}
					}
				}

			case "namespace":
				// Handle namespace dependency
				if members, exists := nsMap[value]; exists {
					for _, memberID := range members {
						if !memberID.Equal(entry.ID) { // Avoid self-dependency
							g.AddEdge(memberID, entry.ID, 1, nil)
						}
					}
				}
			}
		}
	}

	// Compute dependency levels
	levels, err := g.DependencyLevels()
	if err != nil {
		// On cycle detection, fall back to lexicographical sort
		sorted := make([]registry.Entry, 0, len(entries))
		sorted = append(sorted, entries...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID.String() < sorted[j].ID.String()
		})
		return sorted, err
	}

	// Build sorted list based on dependency levels
	result := make([]registry.Entry, 0, len(entries))
	allLevels := levels.AllLevels()
	for _, levelNodes := range allLevels {
		// Sort nodes within the level lexicographically
		sort.Slice(levelNodes, func(i, j int) bool {
			return levelNodes[i].String() < levelNodes[j].String()
		})
		for _, node := range levelNodes {
			if entry, exists := entryMap[node]; exists {
				result = append(result, entry)
			}
		}
	}

	return result, nil
}

// LevelSortEntriesByDependency returns entries grouped by dependency level.
// Entries in the same level have no dependencies on each other and can be processed in parallel.
func LevelSortEntriesByDependency(entries []registry.Entry, resolver registry.DependencyResolver) ([][]registry.Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Build dependency graph and mappings
	g := graph.New[registry.ID, any]()
	entryMap := make(map[registry.ID]registry.Entry, len(entries))
	groupMap := make(map[string][]registry.ID)
	nsMap := make(map[string][]registry.ID)

	// First pass: build all mappings
	for _, entry := range entries {
		g.AddNode(entry.ID)
		entryMap[entry.ID] = entry

		explicitGroups := entry.Meta.GetSlice(registry.TagGroups)
		for _, group := range explicitGroups {
			groupMap[group] = append(groupMap[group], entry.ID)
		}

		if entry.ID.NS != "" {
			nsMap[entry.ID.NS] = append(nsMap[entry.ID.NS], entry.ID)
		}
	}

	// Second pass: process all dependencies
	for _, entry := range entries {
		dependencies := entry.Meta.GetSlice(registry.TagDependsOn)

		if resolver != nil {
			dependencies = append(dependencies, resolver.Extract(entry)...)
		}

		for _, dep := range dependencies {
			depType, value := parseDependency(dep)

			switch depType {
			case "direct":
				targetID := resolveDependencyID(entry.ID.NS, value)
				if _, exists := entryMap[targetID]; exists {
					g.AddEdge(targetID, entry.ID, 1, nil)
				}

			case "group":
				if members, exists := groupMap[value]; exists {
					for _, memberID := range members {
						if !memberID.Equal(entry.ID) {
							g.AddEdge(memberID, entry.ID, 1, nil)
						}
					}
				}

			case "namespace":
				if members, exists := nsMap[value]; exists {
					for _, memberID := range members {
						if !memberID.Equal(entry.ID) {
							g.AddEdge(memberID, entry.ID, 1, nil)
						}
					}
				}
			}
		}
	}

	// Compute dependency levels
	levels, err := g.DependencyLevels()
	if err != nil {
		// On cycle, return all entries in single level, sorted deterministically
		sorted := make([]registry.Entry, 0, len(entries))
		sorted = append(sorted, entries...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID.String() < sorted[j].ID.String()
		})
		return [][]registry.Entry{sorted}, err
	}

	// Convert to entry levels
	allLevels := levels.AllLevels()
	result := make([][]registry.Entry, 0, len(allLevels))
	for _, levelNodes := range allLevels {
		sort.Slice(levelNodes, func(i, j int) bool {
			return levelNodes[i].String() < levelNodes[j].String()
		})
		levelEntries := make([]registry.Entry, 0, len(levelNodes))
		for _, node := range levelNodes {
			if entry, exists := entryMap[node]; exists {
				levelEntries = append(levelEntries, entry)
			}
		}
		if len(levelEntries) > 0 {
			result = append(result, levelEntries)
		}
	}

	return result, nil
}

// CreateChangeSetFromEntries creates a ChangeSet consisting of create operations from a list of entries.
// The entries are sorted taking into account all types of dependencies (direct, group, and namespace).
func CreateChangeSetFromEntries(entries []registry.Entry, resolver registry.DependencyResolver) (registry.ChangeSet, error) {
	sorted, err := SortEntriesByDependency(entries, resolver)
	if err != nil {
		return nil, err
	}

	if len(sorted) == 0 {
		return nil, nil
	}

	cs := make(registry.ChangeSet, 0, len(sorted))
	for _, entry := range sorted {
		cs = append(cs, registry.Operation{
			Kind:  registry.EntryCreate,
			Entry: entry,
		})
	}
	return cs, nil
}
