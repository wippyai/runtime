// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/history/checkpoint"
)

func (h *History) checkpoints() checkpoint.Store {
	codec := checkpoint.StateCodec{Encode: h.encodeChangeSet, Decode: h.decodeChangeSet}
	return checkpoint.Store{Versions: "versions", Changesets: "changesets", Checkpoints: "replay_checkpoints", Parameter: func(int) string { return "?" }, Decode: h.decodeChangeSet, EncodeState: codec.EncodeState, DecodeState: codec.DecodeState}
}

// ReplayState restores the authored projection from a checkpoint and its tail.
func (h *History) ReplayState(ctx context.Context, target registry.Version) (*registry.ReplayState, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.checkpoints().Load(ctx, h.db, target.ID())
}

// PrepareReplayState shares the projection read with checkpoint construction.
func (h *History) PrepareReplayState(ctx context.Context, target registry.Version) (*registry.ReplayState, registry.ReplayActivation, error) {
	h.mu.RLock()
	state, prepared, err := h.checkpoints().Prepare(ctx, h.db, target.ID())
	h.mu.RUnlock()
	if err != nil {
		return nil, nil, err
	}
	return state, func(expected registry.Version, resolution *registry.DependencyResolution) error {
		return h.compareAndSetHead(ctx, expected, target, resolution, prepared)
	}, nil
}
