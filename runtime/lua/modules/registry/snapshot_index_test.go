// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// scanEntry is the lookup contract of Snapshot.GetEntry: the first entry in
// snapshot order whose ID satisfies ID.Equal.
func scanEntry(entries []regapi.Entry, id regapi.ID) (regapi.Entry, error) {
	for _, entry := range entries {
		if entry.ID.Equal(id) {
			return entry, nil
		}
	}
	return regapi.Entry{}, fmt.Errorf("entry not found: %s", id)
}

func TestSnapshotGetEntryMatchesEqualContract(t *testing.T) {
	ids := []regapi.ID{
		// Canonical constructors.
		regapi.NewID("app", "alpha"),
		regapi.ParseID("app:beta"),
		regapi.ParseID("plain"),
		regapi.NewID("", "bare"),
		// Name-only values carrying a namespace separator.
		{Name: "app:gamma"},
		{Name: "legacy:delta:with:colons"},
		// Empty namespace prefixes: canonicalization applies once.
		{Name: ":bare"},
		{Name: ":app:epsilon"},
		regapi.ParseID(":app:zeta"),
		// Separator in the name under an explicit namespace.
		{NS: "app", Name: "eta:theta"},
		{NS: "app:eta", Name: "theta"},
		regapi.ParseID("app:eta:theta"),
		// Direct construction without a cached string.
		{NS: "app", Name: "iota"},
		// Empty identifier.
		{},
		{Name: ":"},
	}

	entries := make([]regapi.Entry, 0, 2*len(ids))
	for i, id := range ids {
		entries = append(entries, regapi.Entry{ID: id, Kind: fmt.Sprintf("first-%d", i)})
	}
	for i, id := range ids {
		entries = append(entries, regapi.Entry{ID: id, Kind: fmt.Sprintf("duplicate-%d", i)})
	}
	// Different spellings of one identity: the earliest in snapshot order wins.
	entries = append(entries,
		regapi.Entry{ID: regapi.ID{Name: "app:kappa"}, Kind: "kappa-name-only"},
		regapi.Entry{ID: regapi.NewID("app", "kappa"), Kind: "kappa-canonical"},
		regapi.Entry{ID: regapi.NewID("app", "lambda"), Kind: "lambda-canonical"},
		regapi.Entry{ID: regapi.ID{Name: "app:lambda"}, Kind: "lambda-name-only"},
	)

	queries := append([]regapi.ID{}, ids...)
	queries = append(queries,
		// Spellings of present identities.
		regapi.ID{NS: "app", Name: "gamma"},
		regapi.NewID("app", "gamma"),
		regapi.ParseID("app:gamma"),
		regapi.ID{Name: "app:alpha"},
		regapi.ID{NS: "app", Name: "alpha"},
		regapi.ID{Name: "bare"},
		regapi.ParseID("app:kappa"),
		regapi.ID{Name: "app:kappa"},
		regapi.ParseID("app:lambda"),
		regapi.ID{Name: "app:lambda"},
		// Missing identities, including near misses of the same joined string.
		regapi.ParseID("app:missing"),
		regapi.ParseID("missing"),
		regapi.ID{NS: "app", Name: "gamma:missing"},
		regapi.ID{NS: "APP", Name: "alpha"},
		regapi.ID{NS: "app", Name: ""},
		regapi.ID{NS: "", Name: "app:"},
	)

	snap := &Snapshot{entries: entries}
	for _, query := range queries {
		name := fmt.Sprintf("ns=%q name=%q", query.NS, query.Name)
		wantEntry, wantErr := scanEntry(entries, query)
		gotEntry, gotErr := snap.GetEntry(query)
		if wantErr != nil {
			require.EqualError(t, gotErr, wantErr.Error(), name)
			require.Equal(t, regapi.Entry{}, gotEntry, name)
			continue
		}
		require.NoError(t, gotErr, name)
		require.Equal(t, wantEntry.Kind, gotEntry.Kind, name)
		require.Equal(t, wantEntry.ID, gotEntry.ID, name)
	}
}

func TestSnapshotGetEntryDistinguishesJoinedStringCollisions(t *testing.T) {
	entries := []regapi.Entry{
		{ID: regapi.ID{NS: "a", Name: "b:c"}, Kind: "a|b:c"},
		{ID: regapi.ID{NS: "a:b", Name: "c"}, Kind: "a:b|c"},
	}
	snap := &Snapshot{entries: entries}

	first, err := snap.GetEntry(regapi.ID{NS: "a", Name: "b:c"})
	require.NoError(t, err)
	require.Equal(t, "a|b:c", first.Kind)

	second, err := snap.GetEntry(regapi.ID{NS: "a:b", Name: "c"})
	require.NoError(t, err)
	require.Equal(t, "a:b|c", second.Kind)
}

func TestSnapshotGetEntryEmptySnapshot(t *testing.T) {
	id := regapi.ParseID("app:missing")
	_, wantErr := scanEntry(nil, id)

	entry, err := (&Snapshot{}).GetEntry(id)
	require.EqualError(t, err, wantErr.Error())
	require.Equal(t, regapi.Entry{}, entry)
}

func TestSnapshotGetFromLuaResolvesEqualIDsAndReportsMissing(t *testing.T) {
	snap := &Snapshot{
		entries: regapi.State{
			{ID: regapi.ID{Name: "app:legacy"}, Kind: "first"},
			{ID: regapi.NewID("app", "legacy"), Kind: "duplicate"},
			{ID: regapi.NewID("app", "canonical"), Kind: "canonical"},
		},
		log: zap.NewNop(),
	}
	ctx, release := strictOverlayContext(t,
		"registry.get\x00app:legacy",
		"registry.get\x00app:canonical",
		"registry.get\x00app:missing",
	)
	defer release()

	runSnapshotState(ctx, t, snap, `
		local legacy, legacy_err = snap:get("app:legacy")
		assert(legacy_err == nil and legacy.kind == "first")

		local canonical, canonical_err = snap:get("app:canonical")
		assert(canonical_err == nil and canonical.kind == "canonical")

		local missing, missing_err = snap:get("app:missing")
		assert(missing == nil)
		assert(missing_err:kind() == errors.NOT_FOUND)
		assert(missing_err:message() == "entry not found: app:missing")
	`)
}

func TestSnapshotGetEntryConcurrentReaders(t *testing.T) {
	entries := benchmarkEntries(512)
	snap := &Snapshot{entries: entries}

	var wg sync.WaitGroup
	for reader := range 16 {
		wg.Go(func() {
			for i := range entries {
				want := entries[(i+reader)%len(entries)]
				got, err := snap.GetEntry(want.ID)
				if err != nil || got.ID != want.ID {
					t.Errorf("reader %d: entry %s: got %s, err %v", reader, want.ID.String(), got.ID.String(), err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func benchmarkEntries(n int) []regapi.Entry {
	entries := make([]regapi.Entry, n)
	for i := range entries {
		ns := fmt.Sprintf("platform.module_%03d.api", i%200)
		name := fmt.Sprintf("handler_entry_%05d", i)
		entries[i] = regapi.Entry{ID: regapi.NewID(ns, name), Kind: "function.lua"}
	}
	return entries
}

// benchmarkEntryCount follows the size of a large production registry state.
const benchmarkEntryCount = 7000

func BenchmarkSnapshotGetEntry(b *testing.B) {
	entries := benchmarkEntries(benchmarkEntryCount)
	lookups := []struct {
		name string
		id   regapi.ID
	}{
		{"first", entries[0].ID},
		{"middle", entries[len(entries)/2].ID},
		{"last", entries[len(entries)-1].ID},
		{"parsed_middle", regapi.ParseID(entries[len(entries)/2].ID.String())},
		{"missing", regapi.ParseID("platform.module_000.api:absent")},
	}

	for _, lookup := range lookups {
		b.Run(lookup.name, func(b *testing.B) {
			snap := &Snapshot{entries: entries}
			b.ReportAllocs()
			for b.Loop() {
				_, _ = snap.GetEntry(lookup.id)
			}
		})
	}
}

// BenchmarkSnapshotGetEntryPerSnapshot measures a fresh snapshot serving n
// distinct lookups, the shape of one Lua registry.snapshot() and its reads.
func BenchmarkSnapshotGetEntryPerSnapshot(b *testing.B) {
	entries := benchmarkEntries(benchmarkEntryCount)
	for _, lookups := range []int{1, 2, 5, 10, 25, 100} {
		b.Run(fmt.Sprintf("gets=%d", lookups), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				snap := &Snapshot{entries: entries}
				for i := range lookups {
					_, _ = snap.GetEntry(entries[(i*2477+len(entries)/2)%len(entries)].ID)
				}
			}
		})
	}
}
