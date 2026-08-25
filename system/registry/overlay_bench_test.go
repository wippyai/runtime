// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func BenchmarkApplyOverlayChurn(b *testing.B) {
	for _, stateSize := range []int{2_000, 10_000} {
		b.Run(fmt.Sprintf("entries_%d", stateSize), func(b *testing.B) {
			ctx := context.Background()
			resolver := topology.NewResolver()
			reg := NewRegistry(
				historymem.New(),
				NewTestRunner(),
				topology.NewStateBuilder(zap.NewNop(), resolver),
				resolver,
				zap.NewNop(),
			)
			baseline := make(regapi.State, stateSize)
			for i := range baseline {
				baseline[i] = regapi.Entry{
					ID:   regapi.NewID("bench", fmt.Sprintf("entry_%05d", i)),
					Kind: regapi.EntryKind,
				}
			}
			if err := reg.LoadState(ctx, hostProvenanced(baseline), version.FromParent(nil, regapi.RootVersion)); err != nil {
				b.Fatal(err)
			}
			entry := regapi.Entry{ID: regapi.NewID("runtime", "source"), Kind: regapi.EntryKind}
			generation, err := reg.ApplyOverlay(ctx, "bench:owner", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				entry.Data = payload.New(i)
				generation, err = reg.ApplyOverlay(ctx, "bench:owner", generation, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: entry}})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkResolveDependenciesDenseGroups(b *testing.B) {
	for _, stateSize := range []int{250, 1_000} {
		b.Run(fmt.Sprintf("entries_%d", stateSize), func(b *testing.B) {
			state := make(regapi.StateMap, stateSize)
			for i := 0; i < stateSize; i++ {
				entry := regapi.Entry{
					ID:   regapi.NewID("bench", fmt.Sprintf("entry_%05d", i)),
					Kind: regapi.EntryKind,
					Meta: attrs.NewBagFrom(map[string]any{
						regapi.TagGroups:    []any{"all"},
						regapi.TagDependsOn: []any{"group:all"},
					}),
				}
				state[entry.ID] = entry
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolved := topology.ResolveDependencies(state, nil)
				if len(resolved) != stateSize {
					b.Fatalf("resolved %d entries", len(resolved))
				}
			}
		})
	}
}

func BenchmarkVisitDependenciesDenseGroups(b *testing.B) {
	for _, stateSize := range []int{250, 1_000} {
		b.Run(fmt.Sprintf("entries_%d", stateSize), func(b *testing.B) {
			state := make(regapi.StateMap, stateSize)
			for i := 0; i < stateSize; i++ {
				entry := regapi.Entry{
					ID:   regapi.NewID("bench", fmt.Sprintf("entry_%05d", i)),
					Kind: regapi.EntryKind,
					Meta: attrs.NewBagFrom(map[string]any{
						regapi.TagGroups:    []any{"all"},
						regapi.TagDependsOn: []any{"group:all"},
					}),
				}
				state[entry.ID] = entry
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				edges := 0
				err := topology.VisitDependencies(state, nil, func(_, _ regapi.ID) error {
					edges++
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
				if edges != stateSize*(stateSize-1) {
					b.Fatalf("visited %d edges", edges)
				}
			}
		})
	}
}
