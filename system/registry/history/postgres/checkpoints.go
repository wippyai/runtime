// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"strconv"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/history/checkpoint"
	"github.com/wippyai/runtime/system/schema"
)

func (h *History) checkpoints() checkpoint.Store {
	codec := checkpoint.StateCodec{Encode: h.encodeChangeSet, Decode: h.decodeChangeSet}
	return checkpoint.Store{Versions: h.tables.versions, Changesets: h.tables.changesets, Checkpoints: schema.QuoteIdentifier(h.tables.schemaName) + `."replay_checkpoints"`, Parameter: func(i int) string { return "$" + strconv.Itoa(i) }, Decode: h.decodeChangeSet, EncodeState: codec.EncodeState, DecodeState: codec.DecodeState}
}

// ReplayState restores the authored projection from a checkpoint and its tail.
func (h *History) ReplayState(ctx context.Context, target registry.Version) (*registry.ReplayState, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.checkpoints().Load(ctx, h.db, target.ID())
}
