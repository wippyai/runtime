package __OOOOLD

import "github.com/ponyruntime/pony/api/registry"

// diffEntries calculates the actions needed to go from prevEntries to newEntries.
// (This is a basic implementation, consider optimizing it if needed)
func diffEntries(prevEntries, newEntries []registry.Entry) []registry.Action {
	actions := make([]registry.Action, 0)

	// Build maps for easy lookup
	prevMap := make(map[registry.Path]*registry.Entry)
	for i := range prevEntries {
		prevMap[prevEntries[i].Path] = &prevEntries[i]
	}
	newMap := make(map[registry.Path]*registry.Entry)
	for i := range newEntries {
		newMap[newEntries[i].Path] = &newEntries[i]
	}

	// Find deleted and updated entries
	for path, prevEntry := range prevMap {
		if newEntry, ok := newMap[path]; !ok {
			actions = append(actions, registry.Action{Kind: registry.Delete, Entry: *prevEntry})
		} else if newEntry.Data != prevEntry.Data { // todO; || newEntry.Meta != prevEntry.Meta
			actions = append(actions, registry.Action{Kind: registry.Update, Entry: *newEntry})
		}
	}

	// Find created entries
	for path, newEntry := range newMap {
		if _, ok := prevMap[path]; !ok {
			actions = append(actions, registry.Action{Kind: registry.Create, Entry: *newEntry})
		}
	}

	return actions
}
