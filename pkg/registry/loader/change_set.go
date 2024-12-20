package loader

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/path"
)

// CreateChangeSetFromEntries creates a ChangeSet of create operations from a list of entries,
// sorted by path.
func CreateChangeSetFromEntries(entries []registry.Entry) registry.ChangeSet {

	if len(entries) == 0 {
		return nil
	}

	paths := make([]registry.ID, 0, len(entries))
	entryMap := make(map[registry.ID]registry.Entry, len(entries))

	for _, entry := range entries {
		paths = append(paths, entry.ID)
		entryMap[entry.ID] = entry
	}

	sortedPaths := path.SortPaths(paths)

	cs := make(registry.ChangeSet, 0, len(entries))
	for _, p := range sortedPaths {
		cs = append(cs, registry.Operation{
			Kind:  registry.Create,
			Entry: entryMap[p],
		})
	}
	return cs
}
