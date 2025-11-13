package lock

// Diff calculates the differences between two lock files.
// Returns a Changes struct with installed, updated, and removed modules.
func Diff(old, new *Lock) *Changes {
	changes := &Changes{
		Installed: []Module{},
		Updated:   []ModuleChange{},
		Removed:   []Module{},
	}

	oldModules := make(map[string]Module)
	for _, mod := range old.data.Modules {
		oldModules[mod.Name] = mod
	}

	newModules := make(map[string]Module)
	for _, mod := range new.data.Modules {
		newModules[mod.Name] = mod
	}

	for name, newMod := range newModules {
		oldMod, existed := oldModules[name]
		if !existed {
			changes.Installed = append(changes.Installed, newMod)
		} else if oldMod.Version != newMod.Version || oldMod.Hash != newMod.Hash {
			changes.Updated = append(changes.Updated, ModuleChange{
				Name:       name,
				OldVersion: oldMod.Version,
				NewVersion: newMod.Version,
				OldHash:    oldMod.Hash,
				NewHash:    newMod.Hash,
			})
		}
	}

	for name, oldMod := range oldModules {
		if _, exists := newModules[name]; !exists {
			changes.Removed = append(changes.Removed, oldMod)
		}
	}

	return changes
}
