// SPDX-License-Identifier: MPL-2.0

package registry

import "github.com/wippyai/runtime/api/registry"

// appendDependencyReplayChanges retains only the dependency operations from
// the existing history pass. A compact delete is enriched from its original
// entry for owner/component classification, without changing stored history.
func appendDependencyReplayChanges(replayed []registry.ChangeSet, changes registry.ChangeSet, state registry.StateMap) []registry.ChangeSet {
	var transaction registry.ChangeSet
	for _, operation := range changes {
		entry := operation.Entry
		if operation.Kind == registry.EntryDelete {
			if operation.OriginalEntry != nil {
				entry = *operation.OriginalEntry
			} else if original, ok := state[entry.ID]; ok {
				entry = original
			}
		}
		wasDependency := operation.OriginalEntry != nil && operation.OriginalEntry.Kind == registry.NamespaceDependency
		if entry.Kind != registry.NamespaceDependency && !wasDependency {
			continue
		}
		operation.Entry = entry
		transaction = append(transaction, operation)
	}
	if len(transaction) > 0 {
		replayed = append(replayed, transaction)
	}
	return replayed
}
