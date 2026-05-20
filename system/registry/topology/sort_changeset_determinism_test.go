// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/testutil"
	"go.uber.org/zap"
)

// TestSortChangeSet_InputOrderInvariant proves that SortChangeSet produces an
// identical output regardless of how the caller orders the input slice.
// stableTopologicalOrder breaks ties between unrelated operations by their
// index in the input changeSet. Pre-fix, an upstream caller iterating a Go map
// would pass entries in hash-seed order, leaking that randomness into the
// sorted result. Post-fix, the function normalizes its input by
// (entry.ID, kind) before computing constraint indexes.
func TestSortChangeSet_InputOrderInvariant(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	canonical := make(registry.ChangeSet, 0, 30)
	for i := 0; i < 30; i++ {
		canonical = append(canonical, registry.Operation{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   registry.NewID("app", fmt.Sprintf("svc-%03d", i)),
				Kind: "service",
				Meta: map[string]any{},
				Data: payload.NewString(fmt.Sprintf("data-%03d", i)),
			},
		})
	}

	var baseline registry.ChangeSet
	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(canonical, seed)
		got, err := builder.SortChangeSet(nil, shuffled)
		require.NoError(t, err, "seed=%d", seed)

		if seed == 0 {
			baseline = got
			continue
		}
		require.Equal(t, baseline, got, "SortChangeSet diverged for seed=%d", seed)
	}
}

// TestSortChangeSet_InputOrderInvariantWithDependencies extends the
// invariant to operations that do have dependency edges. The topological
// constraints must still be honored, and ties within the same dependency
// level must resolve in a deterministic, lexicographic order.
func TestSortChangeSet_InputOrderInvariantWithDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	makeEntry := func(name string, deps ...string) registry.Entry {
		return registry.Entry{
			ID:   registry.NewID("app", name),
			Kind: "service",
			Meta: map[string]any{
				registry.TagDependsOn: deps,
			},
			Data: payload.NewString(name),
		}
	}

	canonical := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: makeEntry("alpha")},
		{Kind: registry.EntryCreate, Entry: makeEntry("bravo", "alpha")},
		{Kind: registry.EntryCreate, Entry: makeEntry("charlie", "alpha")},
		{Kind: registry.EntryCreate, Entry: makeEntry("delta", "bravo", "charlie")},
		{Kind: registry.EntryCreate, Entry: makeEntry("echo")},
		{Kind: registry.EntryCreate, Entry: makeEntry("foxtrot", "echo")},
	}

	var baseline registry.ChangeSet
	for seed := uint64(0); seed < 500; seed++ {
		shuffled := testutil.ShuffleSlice(canonical, seed)
		got, err := builder.SortChangeSet(nil, shuffled)
		require.NoError(t, err)

		// Every dependency must appear before its dependent in the output.
		positions := make(map[string]int, len(got))
		for i, op := range got {
			positions[op.Entry.ID.Name] = i
		}
		require.Less(t, positions["alpha"], positions["bravo"])
		require.Less(t, positions["alpha"], positions["charlie"])
		require.Less(t, positions["bravo"], positions["delta"])
		require.Less(t, positions["charlie"], positions["delta"])
		require.Less(t, positions["echo"], positions["foxtrot"])

		if seed == 0 {
			baseline = got
			continue
		}
		require.Equal(t, baseline, got, "seed=%d", seed)
	}
}
