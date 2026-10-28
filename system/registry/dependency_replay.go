// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/history/checkpoint"
	"github.com/wippyai/runtime/system/registry/topology"
)

func replayHistoryState(ctx context.Context, history registry.History, target registry.Version) (*registry.ReplayState, error) {
	if projection, ok := history.(registry.ProjectionHistory); ok {
		return projection.ReplayState(ctx, target)
	}
	state := registry.NewReplayState()
	if target == nil || target.ID() == registry.RootVersion {
		return state, nil
	}
	transcoder := checkpoint.NewReplayTranscoder()
	apply := func(changes registry.ChangeSet) error { return state.ApplyWithTranscoder(changes, transcoder) }
	if replayer, ok := history.(registry.ChangeSetReplayer); ok {
		if err := replayer.ReplayChanges(ctx, target, apply); err != nil {
			return nil, err
		}
		return state, nil
	}
	var lineage []registry.Version
	for current := target; current != nil && current.ID() > registry.RootVersion; current = current.Previous() {
		lineage = append(lineage, current)
	}
	for i := len(lineage) - 1; i >= 0; i-- {
		changes, err := history.Get(lineage[i])
		if err != nil {
			return nil, err
		}
		if err := apply(changes); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func composeReplayState(baseline registry.State, state *registry.ReplayState) registry.StateMap {
	composed := topology.NewStateMap(baseline)
	for id, entry := range state.Entries {
		composed[canonicalEntryID(id)] = entry
	}
	for id := range state.Tombstones {
		delete(composed, canonicalEntryID(id))
	}
	return composed
}
