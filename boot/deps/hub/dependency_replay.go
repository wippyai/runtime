// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

// replayOwnedDependencyChanges applies edits newer than the last selection of
// their root-package owner. Earlier pins were replaced by that artifact's
// closure (#889), whereas later edits remain authored history. This runs only
// during restore/version transitions, using the registry's existing replay.
func (h *DependencyHandler) replayOwnedDependencyChanges(ctx context.Context, state regapi.State, resolution *regapi.DependencyResolution, transcoder payload.Transcoder) (regapi.State, error) {
	overrides, err := h.ownedDependencyChanges(ctx, resolution, transcoder)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return state, nil
	}
	residentOwners := make(map[string]struct{})
	for _, entry := range state {
		if owner := entryModule(entry); h.isDeploymentRoot(owner) {
			residentOwners[owner] = struct{}{}
		}
	}
	for id, operation := range overrides {
		if _, resident := residentOwners[entryModule(operation.Entry)]; !resident {
			delete(overrides, id)
		}
	}
	if len(overrides) == 0 {
		return state, nil
	}
	// Apply the history in one indexed pass, not one full state copy per edit.
	byID := make(regapi.StateMap, len(state)+len(overrides))
	for _, entry := range state {
		byID[entry.ID] = entry
	}
	for _, operation := range overrides {
		if operation.sameTransaction && operation.Entry.Kind == regapi.NamespaceDependency && operation.Kind != regapi.EntryDelete {
			if current, exists := byID[operation.Entry.ID]; exists && current.Kind == regapi.NamespaceDependency {
				fromArtifact, err := decodeDependency(ctx, transcoder, current)
				if err != nil {
					return nil, err
				}
				authored, err := decodeDependency(ctx, transcoder, operation.Entry)
				if err != nil {
					return nil, err
				}
				if authored.Component == fromArtifact.Component && authored.Version == fromArtifact.Version {
					// Historical journals do not record request ordering within
					// a batch or its final link parameters. Preserve the existing
					// cold-restore precedence for this ambiguous same-batch edit.
					continue
				}
			}
		}
		switch operation.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			byID[operation.Entry.ID] = operation.Entry
		case regapi.EntryDelete:
			delete(byID, operation.Entry.ID)
		}
	}
	restored := make(regapi.State, 0, len(byID))
	for _, entry := range byID {
		restored = append(restored, entry)
	}
	return restored, nil
}

type ownedDependencyChange struct {
	regapi.Operation
	sameTransaction bool
}

// ownedDependencyChanges returns the final effective authored edits, shared by
// exact artifact restore and deployment refresh. It never scans history again.
func (h *DependencyHandler) ownedDependencyChanges(ctx context.Context, resolution *regapi.DependencyResolution, transcoder payload.Transcoder) (map[regapi.ID]ownedDependencyChange, error) {
	changes := regapi.DependencyChangesFromContext(ctx)
	if len(changes) == 0 {
		return nil, nil
	}
	lastOwnerSelection := make(map[string]int)
	for i, transaction := range changes {
		for _, operation := range transaction {
			if operation.Entry.Kind != regapi.NamespaceDependency {
				continue
			}
			definition, err := decodeDependency(ctx, transcoder, operation.Entry)
			if err != nil {
				return nil, err
			}
			if h.isDeploymentRoot(definition.Component) {
				lastOwnerSelection[definition.Component] = i
			}
		}
	}
	overrides := make(map[regapi.ID]ownedDependencyChange)
	for i, transaction := range changes {
		for _, operation := range transaction {
			owner := entryModule(operation.Entry)
			if !h.isDeploymentRoot(owner) {
				continue
			}
			last, selected := lastOwnerSelection[owner]
			if selected && i < last {
				continue
			}
			overrides[operation.Entry.ID] = ownedDependencyChange{Operation: operation, sameTransaction: selected && i == last}
		}
	}
	if len(overrides) == 0 {
		return nil, nil
	}
	// The saved partition also tells which edits survived an owner update in
	// the same transaction. The journal is dependency-sorted, not request-ordered.
	if resolution != nil {
		roots := make(map[string]regapi.DependencyRoot, len(resolution.Roots)+len(resolution.References))
		for _, root := range resolution.Roots {
			roots[root.ID] = root
		}
		for _, reference := range resolution.References {
			roots[reference.ID] = reference
		}
		for id, operation := range overrides {
			root, recorded := roots[id.String()]
			if operation.Kind == regapi.EntryDelete || operation.Entry.Kind != regapi.NamespaceDependency {
				if recorded {
					delete(overrides, id)
				}
				continue
			}
			definition, err := decodeDependency(ctx, transcoder, operation.Entry)
			if err != nil {
				return nil, err
			}
			if !recorded || definition.Component != root.Component || definition.Version != root.Version {
				delete(overrides, id)
			}
		}
	}
	return overrides, nil
}
