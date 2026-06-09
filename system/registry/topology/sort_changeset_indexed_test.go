// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"testing"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
)

func indexedTestBuilder() (*StateBuilder, *Resolver) {
	resolver := NewResolver()
	_ = resolver.RegisterPattern(registry.DependencyPattern{Path: "data.imports", AllowWildcard: true})
	_ = resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.depends_on", AllowWildcard: false})
	return NewStateBuilder(zap.NewNop(), resolver), resolver
}

func makeLargeStateWithReferences(size int) registry.State {
	state := make(registry.State, 0, size)
	for i := 0; i < size; i++ {
		entry := registry.Entry{
			ID:   registry.NewID("app", fmt.Sprintf("entry-%d", i)),
			Kind: "test.entry",
			Meta: map[string]any{"index": i},
		}
		if i > 0 {
			entry.Meta["depends_on"] = []any{fmt.Sprintf("app:entry-%d", i-1)}
		}
		state = append(state, entry)
	}
	return state
}

// BenchmarkSortChangeSetSingleDelete compares the legacy slow path (every
// call rebuilds the inverse-dep index from scratch) against the new indexed
// fast path (long-lived index). The kickside reproducer is a single DELETE
// op against a state of ~2700 entries, where the legacy path was clocked at
// ~4 seconds per call. The indexed path target is < 50ms.
func BenchmarkSortChangeSetSingleDelete(b *testing.B) {
	const stateSize = 2500

	builder, resolver := indexedTestBuilder()
	state := makeLargeStateWithReferences(stateSize)
	delID := registry.NewID("app", "entry-0")
	changeSet := registry.ChangeSet{{
		Kind:  registry.EntryDelete,
		Entry: registry.Entry{ID: delID, Kind: "test.entry"},
	}}

	b.Run("legacy_full_scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := builder.SortChangeSet(state, changeSet); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("indexed_fast_path", func(b *testing.B) {
		depIdx := BuildDepIndex(state, resolver)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := builder.SortChangeSetWithIndex(state, changeSet, depIdx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestSortChangeSetWithIndex_DeleteWithoutDependents(t *testing.T) {
	builder, resolver := indexedTestBuilder()
	state := registry.State{
		{ID: registry.NewID("app", "lone"), Kind: "test.entry"},
		{ID: registry.NewID("app", "other"), Kind: "test.entry"},
	}
	depIdx := BuildDepIndex(state, resolver)
	cs := registry.ChangeSet{{
		Kind:  registry.EntryDelete,
		Entry: registry.Entry{ID: state[0].ID, Kind: "test.entry"},
	}}

	out, err := builder.SortChangeSetWithIndex(state, cs, depIdx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].Kind != registry.EntryDelete {
		t.Fatalf("expected the single delete op, got %+v", out)
	}
}

func TestSortChangeSetWithIndex_DeletesDependentBeforeDependency(t *testing.T) {
	builder, resolver := indexedTestBuilder()
	state := registry.State{
		{
			ID:   registry.NewID("app", "dep"),
			Kind: "test.entry",
		},
		{
			ID:   registry.NewID("app", "consumer"),
			Kind: "test.entry",
			Meta: map[string]any{"depends_on": []any{"app:dep"}},
		},
	}
	depIdx := BuildDepIndex(state, resolver)
	cs := registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: registry.Entry{ID: state[0].ID, Kind: "test.entry"}},
		{Kind: registry.EntryDelete, Entry: registry.Entry{ID: state[1].ID, Kind: "test.entry"}},
	}

	out, err := builder.SortChangeSetWithIndex(state, cs, depIdx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(out))
	}
	if out[0].Entry.ID.Name != "consumer" || out[1].Entry.ID.Name != "dep" {
		t.Fatalf("expected consumer deleted before dep, got %q then %q",
			out[0].Entry.ID.Name, out[1].Entry.ID.Name)
	}
}

func TestSortChangeSetWithIndex_GroupConsumerSortsFirst(t *testing.T) {
	builder, resolver := indexedTestBuilder()
	state := registry.State{
		{
			ID:   registry.NewID("app", "leader"),
			Kind: "test.entry",
			Meta: map[string]any{"groups": []any{"workers"}},
		},
		{
			ID:   registry.NewID("app", "follower"),
			Kind: "test.entry",
			Meta: map[string]any{"depends_on": []any{"group:workers"}},
		},
	}
	depIdx := BuildDepIndex(state, resolver)
	cs := registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: registry.Entry{ID: state[0].ID, Kind: "test.entry"}},
		{Kind: registry.EntryDelete, Entry: registry.Entry{ID: state[1].ID, Kind: "test.entry"}},
	}

	out, err := builder.SortChangeSetWithIndex(state, cs, depIdx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Entry.ID.Name != "follower" || out[1].Entry.ID.Name != "leader" {
		t.Fatalf("group consumer must sort before the group member, got %q then %q",
			out[0].Entry.ID.Name, out[1].Entry.ID.Name)
	}
}

func TestSortChangeSetWithIndex_MatchesLegacyOnRealisticStates(t *testing.T) {
	builder, resolver := indexedTestBuilder()
	state := makeLargeStateWithReferences(50)

	cases := map[string]registry.ChangeSet{
		"single_delete_head": {{
			Kind:  registry.EntryDelete,
			Entry: registry.Entry{ID: registry.NewID("app", "entry-0"), Kind: "test.entry"},
		}},
		"middle_delete_with_consumer": {{
			Kind:  registry.EntryDelete,
			Entry: registry.Entry{ID: registry.NewID("app", "entry-10"), Kind: "test.entry"},
		}, {
			Kind:  registry.EntryDelete,
			Entry: registry.Entry{ID: registry.NewID("app", "entry-11"), Kind: "test.entry"},
		}},
	}

	depIdx := BuildDepIndex(state, resolver)
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			legacy, err := builder.SortChangeSet(state, cs)
			if err != nil {
				t.Fatalf("legacy returned error: %v", err)
			}
			indexed, err := builder.SortChangeSetWithIndex(state, cs, depIdx)
			if err != nil {
				t.Fatalf("indexed returned error: %v", err)
			}
			if len(legacy) != len(indexed) {
				t.Fatalf("op count mismatch legacy=%d indexed=%d", len(legacy), len(indexed))
			}
			for i := range legacy {
				if !legacy[i].Entry.ID.Equal(indexed[i].Entry.ID) || legacy[i].Kind != indexed[i].Kind {
					t.Fatalf("op %d mismatch legacy=%+v indexed=%+v", i, legacy[i], indexed[i])
				}
			}
		})
	}
}

func TestDepIndex_PatchReflectsCreateUpdateDelete(t *testing.T) {
	resolver := NewResolver()
	_ = resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.depends_on"})

	state := registry.State{
		{ID: registry.NewID("app", "dep"), Kind: "test.entry"},
	}
	depIdx := BuildDepIndex(state, resolver)

	newEntry := registry.Entry{
		ID:   registry.NewID("app", "consumer"),
		Kind: "test.entry",
		Meta: map[string]any{"depends_on": []any{"app:dep"}},
	}
	depIdx.Patch(registry.ChangeSet{{Kind: registry.EntryCreate, Entry: newEntry}}, resolver)

	got := make(map[registry.ID]struct{})
	depIdx.Dependents(state[0], got)
	if _, ok := got[newEntry.ID]; !ok {
		t.Fatalf("Patch(create) did not register consumer as a dependent of dep, got %v", got)
	}

	depIdx.Patch(registry.ChangeSet{{Kind: registry.EntryDelete, Entry: newEntry}}, resolver)
	got = make(map[registry.ID]struct{})
	depIdx.Dependents(state[0], got)
	if len(got) != 0 {
		t.Fatalf("Patch(delete) did not clear the consumer entry, got %v", got)
	}
}
