// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
)

var targetEntriesSink []*registry.Entry

func TestFindTargetEntriesAllocationBudget(t *testing.T) {
	entries := make([]registry.Entry, 10000)
	for i := range entries {
		entries[i].ID = registry.NewID("app", fmt.Sprintf("entry_%d", i))
	}
	stage := &linkStage{}
	allocs := testing.AllocsPerRun(20, func() {
		targetEntriesSink = stage.findTargetEntries("app:entry_9999", "other", &entries)
	})
	if allocs > 1 {
		t.Fatalf("qualified lookup allocated %.0f objects; only the result slice should allocate", allocs)
	}
}

func TestFindTargetEntriesKeepsPointersAndOrder(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.ID{NS: "app", Name: "boot"}},
		{ID: registry.ID{NS: "other", Name: "boot"}},
		{ID: registry.ID{NS: "app", Name: "boot"}},
		{ID: registry.ID{NS: "app", Name: "a:b"}},
		{ID: registry.ID{NS: "", Name: "boot"}},
		{ID: registry.ID{NS: "app", Name: ""}},
		{ID: registry.ID{NS: "", Name: ""}},
	}
	stage := &linkStage{}
	for _, tc := range []struct {
		target string
		ns     string
		want   []int
	}{
		{"", "app", nil}, {"absent", "app", nil},
		{"boot", "app", []int{0, 2}}, {"boot", "other", []int{1}},
		{"app:boot", "other", []int{0, 2}}, {"app:a:b", "other", []int{3}},
		{":boot", "app", []int{4}}, {"app:", "other", []int{5}},
		{":", "app", []int{6}}, {" app:boot", "app", nil}, {"app:boot ", "app", nil},
	} {
		t.Run(tc.target+"/"+tc.ns, func(t *testing.T) {
			got := stage.findTargetEntries(tc.target, tc.ns, &entries)
			if len(got) != len(tc.want) || (got == nil) != (tc.want == nil) {
				t.Fatalf("unexpected result shape: %v", got)
			}
			for i, index := range tc.want {
				if got[i] != &entries[index] {
					t.Fatalf("result %d no longer points to original entry %d", i, index)
				}
			}
		})
	}
}

func BenchmarkFindTargetEntries10000(b *testing.B) {
	entries := make([]registry.Entry, 10000)
	for i := range entries {
		entries[i].ID = registry.NewID("app", fmt.Sprintf("entry_%d", i))
	}
	stage := &linkStage{}
	b.ReportAllocs()
	for b.Loop() {
		targetEntriesSink = stage.findTargetEntries("app:entry_9999", "other", &entries)
	}
}
