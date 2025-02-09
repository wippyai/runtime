package loader

import (
	"sort"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
)

// SortEntriesByDependency sorts entries based on both individual and group-level dependencies.
// It uses the pluralized metadata keys "groups" and "depends_on_groups".
func SortEntriesByDependency(entries []registry.Entry) []registry.Entry {
	if len(entries) == 0 {
		return nil
	}

	// Build dependency graph and group mapping.
	g := graph.New[registry.ID]()
	entryMap := make(map[registry.ID]registry.Entry, len(entries))
	groupMap := make(map[string][]registry.ID)

	// Add all entries as nodes and build the group mapping.
	for _, entry := range entries {
		g.AddNode(entry.ID)
		entryMap[entry.ID] = entry

		groups := entry.Meta.TagValue(registry.GroupsTag)
		for _, group := range groups {
			groupMap[group] = append(groupMap[group], entry.ID)
		}
	}

	// Add individual dependency edges.
	for _, entry := range entries {
		dependsOn := entry.Meta.TagValue(registry.DependsOnTag)
		for _, dep := range dependsOn {
			// parse

			if _, exists := entryMap[registry.ParseID(dep)]; exists {
				// If A depends on B, add edge B -> A so B gets processed first.
				g.AddEdge(registry.ParseID(dep), entry.ID, 1)
			}
		}
	}

	// Add group dependency edges.
	for _, entry := range entries {
		dependsOnGroups := entry.Meta.TagValue(registry.DependsOnGroupsTag)
		for _, depGroup := range dependsOnGroups {
			if members, exists := groupMap[depGroup]; exists {
				for _, memberID := range members {
					// Avoid self-dependency.
					if memberID == entry.ID {
						continue
					}
					g.AddEdge(memberID, entry.ID, 1)
				}
			}
		}
	}

	// Compute dependency levels.
	levels, err := g.DependencyLevels()
	if err != nil {
		// On cycle detection, fall back to lexicographical sort.
		sorted := make([]registry.Entry, 0, len(entries))
		sorted = append(sorted, entries...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID.String() < sorted[j].ID.String()
		})
		return sorted
	}

	// Build sorted list based on dependency levels.
	result := make([]registry.Entry, 0, len(entries))
	allLevels := levels.AllLevels()
	for _, levelNodes := range allLevels {
		// Sort nodes within the level lexicographically.
		sort.Slice(levelNodes, func(i, j int) bool {
			return levelNodes[i].String() < levelNodes[j].String()
		})
		for _, node := range levelNodes {
			if entry, exists := entryMap[node]; exists {
				result = append(result, entry)
			}
		}
	}

	return result
}

// CreateChangeSetFromEntries creates a ChangeSet consisting of create operations from a list of entries.
// The entries are sorted taking into account individual and group dependencies.
func CreateChangeSetFromEntries(entries []registry.Entry) registry.ChangeSet {
	sorted := SortEntriesByDependency(entries)
	if len(sorted) == 0 {
		return nil
	}

	cs := make(registry.ChangeSet, 0, len(sorted))
	for _, entry := range sorted {
		cs = append(cs, registry.Operation{
			Kind:  registry.Create,
			Entry: entry,
		})
	}
	return cs
}
