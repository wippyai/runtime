// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

func TestApplyOperationBranchIsolation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	a := registry.Entry{ID: registry.NewID("bee", "a"), Kind: "old"}
	root := StateMap{a.ID: a}
	updated := a
	updated.Kind = "old"
	updated.Meta = map[string]any{"value": "updated"}
	left, err := builder.ApplyOperation(root, registry.Operation{Kind: registry.EntryUpdate, Entry: updated})
	require.NoError(t, err)
	right, err := builder.ApplyOperation(root, registry.Operation{Kind: registry.EntryDelete, Entry: a})
	require.NoError(t, err)
	require.Equal(t, a, root[a.ID])
	require.Equal(t, updated, left[a.ID])
	require.Empty(t, right)
	created := registry.Entry{ID: registry.NewID("bee", "b"), Kind: "new"}
	child, err := builder.ApplyOperation(left, registry.Operation{Kind: registry.EntryCreate, Entry: created})
	require.NoError(t, err)
	delete(child, a.ID)
	right[created.ID] = created
	require.Len(t, root, 1)
	require.Len(t, left, 1)
	require.Equal(t, updated, left[a.ID])
	invalid, err := builder.ApplyOperation(left, registry.Operation{Kind: registry.EntryCreate, Entry: updated})
	require.Error(t, err)
	require.Equal(t, left, invalid)
	require.Len(t, left, 1)
}

func TestPrivateStateOperationPreservesValidationAndSeparateSnapshots(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	a := registry.Entry{ID: registry.NewID("bee", "a"), Kind: "old"}
	root := StateMap{a.ID: a}
	working := CopyStateMap(root)
	updated := a
	updated.Meta = map[string]any{"revision": 2}
	require.NoError(t, builder.ApplyOperationToPrivateState(working, registry.Operation{Kind: registry.EntryUpdate, Entry: updated}))
	require.Equal(t, a, root[a.ID])
	require.Equal(t, updated, working[a.ID])
	invalid := updated
	invalid.Kind = "changed-kind"
	require.Error(t, builder.ApplyOperationToPrivateState(working, registry.Operation{Kind: registry.EntryUpdate, Entry: invalid}))
	require.Equal(t, updated, working[a.ID])
	require.Error(t, builder.ApplyOperationToPrivateState(working, registry.Operation{Kind: registry.EntryCreate, Entry: a}))
	require.Equal(t, updated, working[a.ID])
	require.NoError(t, builder.ApplyOperationToPrivateState(working, registry.Operation{Kind: registry.EntryDelete, Entry: a}))
	require.Empty(t, working)
	require.Equal(t, a, root[a.ID])
}

func BenchmarkPrivateBootStateOperations(b *testing.B) {
	entries := makeTestEntries(1000)
	builder := NewStateBuilder(zap.NewNop(), nil)
	for _, private := range []bool{false, true} {
		name := "snapshots"
		if private {
			name = "private"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				state := make(StateMap, len(entries))
				for _, entry := range entries {
					op := registry.Operation{Kind: registry.EntryCreate, Entry: entry}
					if private {
						if err := builder.ApplyOperationToPrivateState(state, op); err != nil {
							b.Fatal(err)
						}
					} else {
						var err error
						state, err = builder.ApplyOperation(state, op)
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}

func TestBuildStateReplayIsolationAndValidation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	a := registry.Entry{ID: registry.NewID("bee", "a"), Kind: "lua.library"}
	updated := a
	updated.Meta = map[string]any{"revision": 2}
	changes := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: a},
		{Kind: registry.EntryUpdate, Entry: updated},
	}
	h := &bootHistory{MockHistory: NewMockHistory(), changes: changes}
	target := version.New(registry.RootVersion)
	first, err := builder.BuildState(h, target)
	require.NoError(t, err)
	first[0] = registry.Entry{}
	second, err := builder.BuildState(h, target)
	require.NoError(t, err)
	require.Equal(t, registry.State{updated}, second)
	require.Equal(t, a, h.changes[0].Entry)
	require.Equal(t, updated, h.changes[1].Entry)
	// The fallback history implementation must have the same result.
	fallback := NewMockHistory()
	require.NoError(t, fallback.Save(target, changes, true))
	fromFallback, err := builder.BuildState(fallback, target)
	require.NoError(t, err)
	require.Equal(t, second, fromFallback)
	h.changes = append(h.changes, registry.Operation{Kind: registry.EntryDelete, Entry: updated})
	deleted, err := builder.BuildState(h, target)
	require.NoError(t, err)
	require.Empty(t, deleted)
	require.Equal(t, registry.State{updated}, second)
	h.changes = append(h.changes, registry.Operation{Kind: registry.EntryUpdate, Entry: updated})
	failed, err := builder.BuildState(h, target)
	require.Error(t, err)
	require.Nil(t, failed)
	require.Equal(t, registry.State{updated}, second)
}
