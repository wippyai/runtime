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
	bindings := resolutionBindingIndex(resolution)
	for _, operation := range overrides {
		_, recordedBinding := bindings[operation.Entry.ID.String()]
		if !recordedBinding && operation.sameTransaction && operation.Entry.Kind == regapi.NamespaceDependency && operation.Kind != regapi.EntryDelete {
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
	replay, err := regapi.DependencyReplayState(ctx, transcoder)
	if err != nil {
		return nil, err
	}
	return h.ownedDependencyReplayState(ctx, replay, resolution, transcoder)
}

// ownedDependencyReplayState projects compact replay evidence into the owned
// authored edits that survive the final selection of a deployment root.
func (h *DependencyHandler) ownedDependencyReplayState(ctx context.Context, replay *regapi.ReplayState, resolution *regapi.DependencyResolution, transcoder payload.Transcoder) (map[regapi.ID]ownedDependencyChange, error) {
	baseline, _ := regapi.DependencyBaselineFromContext(ctx)
	selections, err := replay.DependencySelectionsWithBaseline(baseline, transcoder)
	if err != nil {
		return nil, err
	}
	overrides := make(map[regapi.ID]ownedDependencyChange)
	for id, recorded := range replay.LastDependencyOperations(baseline) {
		owner := entryModule(recorded.Entry)
		if !h.isDeploymentRoot(owner) {
			continue
		}
		selection, selected := selections[owner]
		if selected && recorded.Transaction < selection.Transaction {
			continue
		}
		overrides[id] = ownedDependencyChange{
			Operation:       recorded.Operation,
			sameTransaction: selected && recorded.Transaction == selection.Transaction,
		}
	}
	return h.filterOwnedDependencyChanges(ctx, overrides, resolution, transcoder)
}

func (h *DependencyHandler) filterOwnedDependencyChanges(ctx context.Context, overrides map[regapi.ID]ownedDependencyChange, resolution *regapi.DependencyResolution, transcoder payload.Transcoder) (map[regapi.ID]ownedDependencyChange, error) {
	if len(overrides) == 0 {
		return nil, nil
	}
	if resolution == nil {
		return overrides, nil
	}
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
	return overrides, nil
}

func hasDependencyReplayChanges(ctx context.Context) bool {
	if replay, ok := regapi.DependencyReplayStateFromContext(ctx); ok {
		baseline, _ := regapi.DependencyBaselineFromContext(ctx)
		return len(replay.LastDependencyOperations(baseline)) > 0
	}
	return len(regapi.DependencyChangesFromContext(ctx)) > 0
}
