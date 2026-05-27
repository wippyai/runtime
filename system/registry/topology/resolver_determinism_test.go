// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/testutil"
)

// TestExtract_StableOutput is the regression test for Resolver.Extract()
// returning dependencies in a stable order. Pre-fix Extract built a Go map
// (depSet) then iterated it to produce the slice, so the result was
// hash-seed randomized and leaked non-determinism into every consumer
// (sort_changeset.go:140, sort.go:82/182, supervisor's dependency resolver).
func TestExtract_StableOutput(t *testing.T) {
	resolver := buildDeterminismResolver()

	entry := registry.Entry{
		ID: registry.NewID("app", "service"),
		Meta: attrs.NewBagFrom(map[string]any{
			"depends_on": []string{"app.fs:state", "app.fs:cache", "app.driver:queue"},
			"groups":     []string{"infra", "core"},
		}),
		Data: &mapPayload{data: map[string]any{
			"imports": map[string]any{
				"alpha":   "lib.alpha:mod",
				"bravo":   "lib.bravo:mod",
				"charlie": "lib.charlie:mod",
				"delta":   "lib.delta:mod",
				"echo":    "lib.echo:mod",
				"foxtrot": "lib.foxtrot:mod",
			},
			"fs":     "app.fs:assets",
			"driver": "app.driver:db",
		}},
	}

	baseline := resolver.Extract(entry)
	require.NotEmpty(t, baseline)

	expectedSorted := append([]string(nil), baseline...)
	sort.Strings(expectedSorted)
	require.Equal(t, expectedSorted, baseline, "Extract output must be sorted")

	for i := 0; i < 100; i++ {
		got := resolver.Extract(entry)
		require.Equal(t, baseline, got, "iteration %d", i)
	}
}

// TestExtract_DeterministicAcrossMapInsertionOrders rebuilds the entry's
// data map by inserting keys in 100 different shuffled orders. Because Go
// map iteration order depends on the hash seed (not insertion order),
// shuffling the input does not directly prove determinism — but it does
// exercise the wildcard-driven map iteration inside resolverNavigatePath
// for many distinct map layouts, which is the production code path that
// surfaced the bug.
func TestExtract_DeterministicAcrossMapInsertionOrders(t *testing.T) {
	resolver := buildDeterminismResolver()

	importKeys := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	importValues := map[string]string{
		"alpha":   "lib.alpha:mod",
		"bravo":   "lib.bravo:mod",
		"charlie": "lib.charlie:mod",
		"delta":   "lib.delta:mod",
		"echo":    "lib.echo:mod",
		"foxtrot": "lib.foxtrot:mod",
		"golf":    "lib.golf:mod",
		"hotel":   "lib.hotel:mod",
	}

	var baseline []string

	for seed := uint64(0); seed < 100; seed++ {
		shuffledKeys := testutil.ShuffleSlice(importKeys, seed)
		imports := make(map[string]any, len(shuffledKeys))
		for _, k := range shuffledKeys {
			imports[k] = importValues[k]
		}

		entry := registry.Entry{
			ID: registry.NewID("app", "service"),
			Meta: attrs.NewBagFrom(map[string]any{
				"depends_on": []string{"app.fs:state", "app.fs:cache"},
			}),
			Data: &mapPayload{data: map[string]any{
				"imports": imports,
			}},
		}

		got := resolver.Extract(entry)

		if seed == 0 {
			baseline = got
			expectedSorted := append([]string(nil), baseline...)
			sort.Strings(expectedSorted)
			require.Equal(t, expectedSorted, baseline, "baseline must be sorted")
			continue
		}
		require.Equal(t, baseline, got, "seed=%d", seed)
	}
}

// TestExtract_EmptyEntry confirms the empty path stays nil after the sort.
func TestExtract_EmptyEntry(t *testing.T) {
	resolver := buildDeterminismResolver()
	got := resolver.Extract(registry.Entry{ID: registry.NewID("app", "empty")})
	require.Nil(t, got)
}

// buildDeterminismResolver registers the patterns that exercise both
// meta-side extraction and data-side wildcard traversal — the two flavors
// that fed map iteration before the fix.
func buildDeterminismResolver() *Resolver {
	r := NewResolver()
	patterns := []registry.DependencyPattern{
		{Path: "meta.depends_on", AllowWildcard: true},
		{Path: "meta.groups", AllowWildcard: true},
		{Path: "data.imports.*", AllowWildcard: true},
		{Path: "data.fs"},
		{Path: "data.driver"},
	}
	for _, p := range patterns {
		_ = r.RegisterPattern(p)
	}
	return r
}

// mapPayload is a minimal payload.Payload implementation backed by a Go map.
type mapPayload struct {
	data any
}

func (p *mapPayload) Data() any              { return p.data }
func (p *mapPayload) Format() payload.Format { return payload.Golang }
