// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestOwnedDependencyReplayOrdering(t *testing.T) {
	handler := &DependencyHandler{deployment: &regapi.Deployment{Root: "acme/app"}}
	root := hardeningRoot("host.deps:app", "acme/app", "2.0.0")
	worker := hardeningRoot("app.deps:worker", "acme/worker", "*")
	worker.Registry = regapi.EntryMetadata{Owner: "acme/app", Root: true}
	changed := worker
	changed.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
	definition := hardeningModuleDefinition("acme.app", "acme/app", "2.0.0")
	for _, tc := range []struct {
		name    string
		changes regapi.ChangeSet
		want    regapi.State
	}{
		{name: "owner-replaces-earlier-pin", changes: regapi.ChangeSet{
			{Kind: regapi.EntryUpdate, Entry: changed}, {Kind: regapi.EntryCreate, Entry: root},
		}, want: regapi.State{definition, worker}},
		{name: "later-pin-survives", changes: regapi.ChangeSet{
			{Kind: regapi.EntryCreate, Entry: root}, {Kind: regapi.EntryUpdate, Entry: changed},
		}, want: regapi.State{definition, changed}},
		{name: "later-delete-survives", changes: regapi.ChangeSet{
			{Kind: regapi.EntryCreate, Entry: root}, {Kind: regapi.EntryDelete, Entry: worker},
		}, want: regapi.State{definition}},
		{name: "kind-replacement-survives", changes: regapi.ChangeSet{
			{Kind: regapi.EntryCreate, Entry: root},
			{Kind: regapi.EntryUpdate, Entry: regapi.Entry{ID: worker.ID, Kind: regapi.EntryKind, Registry: worker.Registry}},
		}, want: regapi.State{definition, {ID: worker.ID, Kind: regapi.EntryKind, Registry: worker.Registry}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transactions := make([]regapi.ChangeSet, len(tc.changes))
			for i, operation := range tc.changes {
				transactions[i] = regapi.ChangeSet{operation}
			}
			for _, path := range []string{"raw", "compact"} {
				t.Run(path, func(t *testing.T) {
					ctx := newTestContext()
					if path == "raw" {
						ctx = regapi.WithDependencyBaseline(ctx, nil, transactions)
					} else {
						projection := regapi.NewReplayState()
						for _, transaction := range transactions {
							require.NoError(t, projection.ApplyDependencyChanges(transaction, payload.GetTranscoder(ctx)))
						}
						ctx = regapi.WithDependencyReplayState(ctx, nil, projection)
					}
					got, err := handler.replayOwnedDependencyChanges(ctx, regapi.State{definition, worker}, nil, payload.GetTranscoder(ctx))
					require.NoError(t, err)
					require.ElementsMatch(t, tc.want, got)
				})
			}
		})
	}
	ctx := regapi.WithDependencyBaseline(newTestContext(), nil, []regapi.ChangeSet{{{Kind: regapi.EntryUpdate, Entry: changed}}})
	got, err := handler.replayOwnedDependencyChanges(ctx, nil, nil, payload.GetTranscoder(ctx))
	require.NoError(t, err)
	require.Empty(t, got, "history cannot resurrect a removed owner's entries")
}

func TestExpandReplayGuardRequiresDependencyEvidence(t *testing.T) {
	base := newTestContext()
	for _, tc := range []struct {
		ctx  context.Context
		name string
		want bool
	}{
		{name: "raw-empty", ctx: regapi.WithDependencyBaseline(base, nil, nil), want: false},
		{name: "compact-empty", ctx: regapi.WithDependencyReplayState(base, nil, regapi.NewReplayState()), want: false},
		{name: "raw-config-only", ctx: regapi.WithDependencyBaseline(base, nil, nil), want: false},
		{name: "compact-config-only", ctx: func() context.Context {
			state := regapi.NewReplayState()
			require.NoError(t, state.Apply(base, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: regapi.Entry{ID: regapi.NewID("app", "config"), Kind: regapi.EntryKind}}}))
			return regapi.WithDependencyReplayState(base, nil, state)
		}(), want: false},
		{name: "compact-dependency", ctx: func() context.Context {
			state := regapi.NewReplayState()
			require.NoError(t, state.Apply(base, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: hardeningRoot("app.deps:worker", "acme/worker", "*")}}))
			return regapi.WithDependencyReplayState(base, nil, state)
		}(), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hasDependencyReplayChanges(tc.ctx))
		})
	}
}

func BenchmarkOwnedDependencyReplay(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		for _, count := range []int{0, 75, 1000} {
			b.Run(fmt.Sprintf("entries=%d/changes=%d", size, count), func(b *testing.B) {
				state := make(regapi.State, size)
				for i := range state {
					state[i] = regapi.Entry{ID: regapi.NewID("app", fmt.Sprint(i)), Kind: regapi.EntryKind,
						Registry: regapi.EntryMetadata{Owner: "acme/app"}}
				}
				changes := make(regapi.ChangeSet, count)
				for i := range changes {
					entry := hardeningRoot(fmt.Sprintf("app.deps:worker%d", i), "acme/worker", "*")
					entry.Registry.Owner = "acme/app"
					changes[i] = regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry}
				}
				var transactions []regapi.ChangeSet
				if len(changes) > 0 {
					transactions = []regapi.ChangeSet{changes}
				}
				ctx := regapi.WithDependencyBaseline(newTestContext(), nil, transactions)
				handler := &DependencyHandler{deployment: &regapi.Deployment{Root: "acme/app"}}
				transcoder := payload.GetTranscoder(ctx)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := handler.replayOwnedDependencyChanges(ctx, state, nil, transcoder); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
