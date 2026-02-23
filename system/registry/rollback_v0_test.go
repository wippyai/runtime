// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func TestRollbackToV0(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	hist := historymem.New()
	runner := &MockRunner{
		RunFunc: func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
			stateMap := make(map[registry.ID]registry.Entry, len(state))
			for _, entry := range state {
				stateMap[entry.ID] = entry
			}
			for _, op := range changes {
				switch op.Kind {
				case registry.EntryCreate:
					stateMap[op.Entry.ID] = op.Entry
				case registry.EntryUpdate:
					stateMap[op.Entry.ID] = op.Entry
				case registry.EntryDelete:
					delete(stateMap, op.Entry.ID)
				}
			}
			result := make(registry.State, 0, len(stateMap))
			for _, entry := range stateMap {
				result = append(result, entry)
			}
			return result, nil
		},
	}
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("baseline"),
		},
	}

	err := reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	entryID := registry.NewID("test", "entry1")

	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v1"),
		}},
	})
	require.NoError(t, err)

	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v2"),
		}},
	})
	require.NoError(t, err)

	entries, _ := reg.GetAllEntries()
	var foundAtV2 bool
	for _, e := range entries {
		if e.ID == entryID {
			foundAtV2 = true
			assert.Equal(t, "v2", e.Data.Data().(string))
		}
	}
	assert.True(t, foundAtV2, "Entry should exist at v2")

	err = reg.ApplyVersion(ctx, v1)
	require.NoError(t, err)

	entries, _ = reg.GetAllEntries()
	var foundAtV1 bool
	for _, e := range entries {
		if e.ID == entryID {
			foundAtV1 = true
			assert.Equal(t, "v1", e.Data.Data().(string))
		}
	}
	assert.True(t, foundAtV1, "Entry should exist at v1")

	v0 := version.FromParent(nil, 0)
	err = reg.ApplyVersion(ctx, v0)
	require.NoError(t, err, "Should be able to rollback to v0")

	entries, _ = reg.GetAllEntries()
	var foundAtV0 bool
	for _, e := range entries {
		if e.ID == entryID {
			foundAtV0 = true
		}
	}
	assert.False(t, foundAtV0, "Entry should not exist at v0 (only baseline)")

	var baselineFound bool
	for _, e := range entries {
		if e.ID == baseline[0].ID {
			baselineFound = true
		}
	}
	assert.True(t, baselineFound, "Baseline entry should still exist at v0")
}
