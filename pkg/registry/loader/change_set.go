package loader

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
	"log"
	"sort"
)

// SortEntriesByDependency sorts entries based on their dependencies.
// If reverseOrder is true, it sorts in reverse dependency order (useful for deletes).
func SortEntriesByDependency(entries []registry.Entry) []registry.Entry {
	if len(entries) == 0 {
		return nil
	}

	// Build dependency graph
	g := graph.NewGraph()
	entryMap := make(map[registry.ID]registry.Entry, len(entries))

	// Add all entries as nodes and build entry map
	for _, entry := range entries {
		g.AddNode(graph.Node(entry.ID))
		entryMap[entry.ID] = entry
	}

	// Add dependency edges
	for _, entry := range entries {
		dependsOn := entry.Meta.TagValue(registry.DependsOnTag)
		log.Printf("dependsOn: %+v ======>> %+v", entry.Meta, dependsOn)
		for _, dep := range dependsOn {
			// Only consider dependencies within the set of entries we're sorting
			if _, exists := entryMap[registry.ID(dep)]; exists {
				// Normal order: if A depends on B, add edge B->A so B gets processed before A
				g.AddEdge(graph.Edge{
					From:   graph.Node(dep),
					To:     graph.Node(entry.ID),
					Weight: 1,
				})
			}
		}
	}

	// Get dependency levels
	levels, err := g.DependencyLevels()
	if err != nil {
		// If there's a cycle, fall back to lexicographical sorting
		sorted := make([]registry.Entry, 0, len(entries))
		for _, entry := range entries {
			sorted = append(sorted, entry)
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		return sorted
	}

	// Build sorted list based on dependency levels
	result := make([]registry.Entry, 0, len(entries))

	start, end, step := 0, levels.LevelCount(), 1

	for i := start; i != end; i += step {
		levelNodes, _ := levels.GetLevel(i)

		// Sort nodes within each level lexicographically
		sort.Slice(levelNodes, func(i, j int) bool {
			return string(levelNodes[i]) < string(levelNodes[j])
		})

		// Add entries from this level
		for _, node := range levelNodes {
			entryID := registry.ID(node)
			if entry, exists := entryMap[entryID]; exists {
				result = append(result, entry)
			}
		}
	}

	return result
}

// CreateChangeSetFromEntries creates a ChangeSet of create operations from a list of entries,
// sorted by dependencies and path.
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
