// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func makeTestEntries(n int) []registry.Entry {
	entries := make([]registry.Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ID{NS: "test", Name: fmt.Sprintf("entry-%d", i)},
			Kind: "test.entry",
			Meta: map[string]any{"index": i},
		}
	}
	return entries
}

func makeTestChangesets(numSets, entriesPerSet int) []registry.ChangeSet {
	changesets := make([]registry.ChangeSet, numSets)
	for i := 0; i < numSets; i++ {
		cs := make(registry.ChangeSet, entriesPerSet)
		for j := 0; j < entriesPerSet; j++ {
			cs[j] = registry.Operation{
				Kind: registry.EntryUpdate,
				Entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: fmt.Sprintf("entry-%d", j)},
					Kind: "test.entry",
					Meta: map[string]any{"version": i},
				},
			}
		}
		changesets[i] = cs
	}
	return changesets
}

func BenchmarkSquashChangesets(b *testing.B) {
	sizes := []struct {
		numSets       int
		entriesPerSet int
	}{
		{5, 20},   // small: typical changeset
		{10, 100}, // medium
		{20, 500}, // large
		{1, 5000}, // boot scenario: single changeset with all entries
	}

	for _, size := range sizes {
		name := fmt.Sprintf("sets=%d_entries=%d", size.numSets, size.entriesPerSet)
		b.Run(name, func(b *testing.B) {
			log := zap.NewNop()
			builder := NewStateBuilder(log, nil)
			changesets := makeTestChangesets(size.numSets, size.entriesPerSet)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = builder.SquashChangesets(changesets)
			}
		})
	}
}

func BenchmarkBuildDelta(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			log := zap.NewNop()
			builder := NewStateBuilder(log, nil)

			// Create "from" state
			fromEntries := makeTestEntries(size)
			from := registry.State(fromEntries)

			// Create "to" state with some changes
			toEntries := make([]registry.Entry, size)
			copy(toEntries, fromEntries)
			// Modify 10% of entries
			for i := 0; i < size/10; i++ {
				toEntries[i].Meta = map[string]any{"modified": true}
			}
			to := registry.State(toEntries)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = builder.BuildDelta(from, to)
			}
		})
	}
}

func BenchmarkBuildDelta_AllNew(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			log := zap.NewNop()
			builder := NewStateBuilder(log, nil)

			from := registry.State{}
			to := registry.State(makeTestEntries(size))

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = builder.BuildDelta(from, to)
			}
		})
	}
}
