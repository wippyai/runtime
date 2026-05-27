// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/testutil"
	"go.uber.org/zap"
)

// TestSquashChangesets_DeterministicAcrossSeeds is the regression test for the
// map-iteration bug in SquashChangesets(). Pre-fix the function aggregated
// operations in a map[registry.ID]registry.Operation, then iterated the map
// to build the result slice — the slice element order then depended on the Go
// map hash seed, and downstream SortChangeSet inherited that randomness.
// Post-fix the slice is sorted by (entry.ID.NS, entry.ID.Name, kind) before
// SortChangeSet, so the output is identical across runs.
func TestSquashChangesets_DeterministicAcrossSeeds(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	const entryCount = 40

	canonicalChangeset := make(registry.ChangeSet, 0, entryCount)
	for i := 0; i < entryCount; i++ {
		id := registry.NewID("app", fmt.Sprintf("svc-%03d", i))
		canonicalChangeset = append(canonicalChangeset, registry.Operation{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   id,
				Kind: "service",
				Meta: map[string]any{},
				Data: payload.NewString(fmt.Sprintf("data-%03d", i)),
			},
		})
	}

	var baseline registry.ChangeSet

	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(canonicalChangeset, seed)
		got := builder.SquashChangesets([]registry.ChangeSet{shuffled})

		if seed == 0 {
			baseline = got
			require.Len(t, baseline, entryCount)
			expectedNames := make([]string, len(baseline))
			for i, op := range baseline {
				expectedNames[i] = op.Entry.ID.Name
			}
			sortedNames := append([]string(nil), expectedNames...)
			sort.Strings(sortedNames)
			require.Equal(t, sortedNames, expectedNames,
				"baseline should be lexicographically sorted across independent entries")
			continue
		}

		require.Equal(t, baseline, got, "squash output diverged for seed=%d", seed)
	}
}

// TestSquashChangesets_DeterministicAcrossMixedKinds covers the more
// nuanced case where the changeset mixes creates, updates, and deletes for
// the same and different IDs. The squashing rules then yield a complex
// internal map population, and the post-fix sort must keep the result
// deterministic for downstream consumers.
func TestSquashChangesets_DeterministicAcrossMixedKinds(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	build := func() []registry.ChangeSet {
		makeEntry := func(name string, generation int) registry.Entry {
			return registry.Entry{
				ID:   registry.NewID("app", name),
				Kind: "service",
				Meta: map[string]any{"gen": generation},
				Data: payload.NewString(fmt.Sprintf("%s-gen-%d", name, generation)),
			}
		}

		return []registry.ChangeSet{
			{
				{Kind: registry.EntryCreate, Entry: makeEntry("alpha", 1)},
				{Kind: registry.EntryCreate, Entry: makeEntry("bravo", 1)},
				{Kind: registry.EntryCreate, Entry: makeEntry("charlie", 1)},
				{Kind: registry.EntryCreate, Entry: makeEntry("delta", 1)},
			},
			{
				{Kind: registry.EntryUpdate, Entry: makeEntry("alpha", 2)},
				{Kind: registry.EntryDelete, Entry: makeEntry("bravo", 1)},
				{Kind: registry.EntryCreate, Entry: makeEntry("echo", 1)},
			},
			{
				{Kind: registry.EntryUpdate, Entry: makeEntry("alpha", 3)},
				{Kind: registry.EntryCreate, Entry: makeEntry("bravo", 2)},
				{Kind: registry.EntryDelete, Entry: makeEntry("delta", 1)},
			},
		}
	}

	baseline := builder.SquashChangesets(build())
	require.NotEmpty(t, baseline)

	for i := 0; i < 100; i++ {
		got := builder.SquashChangesets(build())
		require.Equal(t, baseline, got,
			"squash output for fixed input must be stable across runs (iteration %d)", i)
	}
}
