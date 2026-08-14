// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func newOverlayTestRegistry(t *testing.T) (*Reg, *historymem.Storage) {
	reg, history, _ := newOverlayTestRegistryWithRunner(t)
	return reg, history
}

func newOverlayTestRegistryWithRunner(t *testing.T) (*Reg, *historymem.Storage, *TestRunner) {
	t.Helper()
	history := historymem.New()
	resolver := topology.NewResolver()
	require.NoError(t, resolver.RegisterPattern(regapi.DependencyPattern{
		Path: "meta.depends_on", Description: "test dependencies", AllowWildcard: true,
	}))
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	runner := NewTestRunner()
	return NewRegistry(history, runner, builder, resolver, zap.NewNop()), history, runner
}

func TestOverlayDoesNotAdvanceHistory(t *testing.T) {
	ctx := context.Background()
	reg, history := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))

	entry := regapi.Entry{ID: regapi.NewID("runtime.db", "one"), Kind: "test.resource", Data: payload.New("live")}
	generation, err := reg.ApplyOverlay(ctx, "data-sources:one", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(0), current.ID())
	_, err = reg.GetEntry(entry.ID)
	require.NoError(t, err)
	head, err := history.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(0), head.ID())
}

func TestOverlaySurvivesHistoryCommit(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	runtimeEntry := regapi.Entry{ID: regapi.NewID("runtime.db", "one"), Kind: "test.resource", Data: payload.New("live")}
	_, err := reg.ApplyOverlay(ctx, "data-sources:one", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: runtimeEntry}})
	require.NoError(t, err)

	durable := regapi.Entry{ID: regapi.NewID("app", "setting"), Kind: regapi.EntryKind, Data: payload.New("v1")}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)
	assert.Equal(t, uint(1), v1.ID())
	_, err = reg.GetEntry(runtimeEntry.ID)
	require.NoError(t, err)
	projection, generation, err := reg.GetOverlay("data-sources:one")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
	assert.Len(t, projection, 1)
}

func TestOverlaySurvivesVersionSelection(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	durable := regapi.Entry{ID: regapi.NewID("app", "setting"), Kind: regapi.EntryKind, Data: payload.New("v1")}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)
	durable.Data = payload.New("v2")
	v2, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: durable}})
	require.NoError(t, err)

	runtimeEntry := regapi.Entry{ID: regapi.NewID("runtime.db", "one"), Kind: "test.resource", Data: payload.New("live")}
	_, err = reg.ApplyOverlay(ctx, "data-sources:one", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: runtimeEntry}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	_, err = reg.GetEntry(runtimeEntry.ID)
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v2))
	_, err = reg.GetEntry(runtimeEntry.ID)
	require.NoError(t, err)
}

func TestOverlayCannotShadowOrLeakIntoHistory(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	base := regapi.Entry{ID: regapi.NewID("app", "base"), Kind: regapi.EntryKind, Data: payload.New("base")}
	require.NoError(t, reg.LoadState(ctx, regapi.State{base}, version.FromParent(nil, regapi.RootVersion)))

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: base}})
	require.Error(t, err)
	runtimeEntry := regapi.Entry{ID: regapi.NewID("runtime", "one"), Kind: regapi.EntryKind, Data: payload.New("runtime")}
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: runtimeEntry}})
	require.NoError(t, err)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: runtimeEntry}})
	require.Error(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: runtimeEntry}})
	require.Error(t, err)
}

func TestOverlayRejectsStaleGeneration(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	entry := regapi.Entry{ID: regapi.NewID("runtime", "one"), Kind: regapi.EntryKind, Data: payload.New("v1")}
	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
	entry.Data = payload.New("stale")
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: entry}})
	require.Error(t, err)
	stored, err := reg.GetEntry(entry.ID)
	require.NoError(t, err)
	assert.Equal(t, "v1", stored.Data.Data())
}

func TestOverlayBulkDeleteUsesReverseDependencyOrder(t *testing.T) {
	ctx := context.Background()
	reg, _, runner := newOverlayTestRegistryWithRunner(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	envID := regapi.NewID("runtime.env", "password")
	dbID := regapi.NewID("runtime.db", "source")
	envEntry := regapi.Entry{ID: envID, Kind: "env.variable"}
	dbEntry := regapi.Entry{ID: dbID, Kind: "db.sql.postgres", Meta: attrs.NewBagFrom(map[string]any{"depends_on": envID.String()})}
	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: dbEntry},
		{Kind: regapi.EntryCreate, Entry: envEntry},
	})
	require.NoError(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{
		{Kind: regapi.EntryDelete, Entry: envEntry},
		{Kind: regapi.EntryDelete, Entry: dbEntry},
	})
	require.NoError(t, err)
	last := runner.LastTransition()
	require.Len(t, last, 2)
	assert.Equal(t, dbID, last[0].Entry.ID)
	assert.Equal(t, envID, last[1].Entry.ID)
	entries, nextGeneration, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Equal(t, uint64(2), nextGeneration)
}

func TestOverlayCannotDeleteDependencyWhileDependentSurvives(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	target := regapi.Entry{ID: regapi.NewID("runtime", "target"), Kind: regapi.EntryKind}
	consumer := regapi.Entry{
		ID: regapi.NewID("runtime", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{regapi.TagDependsOn: "target"}),
	}
	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: target},
		{Kind: regapi.EntryCreate, Entry: consumer},
	})
	require.NoError(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: target}})
	require.Error(t, err)
	_, err = reg.GetEntry(target.ID)
	require.NoError(t, err)
}

func TestOverlayRejectsCrossOwnerDependenciesInEitherCreationOrder(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	targetID := regapi.NewID("runtime", "target")
	consumer := regapi.Entry{
		ID: regapi.NewID("runtime", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": targetID.String()}),
	}
	target := regapi.Entry{ID: targetID, Kind: regapi.EntryKind}

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: consumer}})
	require.NoError(t, err, "a same-owner target may be added in a later changeset")
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: target}})
	require.Error(t, err, "a later owner cannot claim an ID referenced by another overlay")

	reg, _ = newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: target}})
	require.NoError(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: consumer}})
	require.Error(t, err, "an overlay cannot depend on an existing foreign overlay")
}

func TestDurableHistoryCannotDependOnOrRemoveOverlayGraph(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	baseID := regapi.NewID("app", "base")
	base := regapi.Entry{ID: baseID, Kind: regapi.EntryKind}
	require.NoError(t, reg.LoadState(ctx, regapi.State{base}, version.FromParent(nil, regapi.RootVersion)))
	overlayID := regapi.NewID("runtime", "one")
	overlay := regapi.Entry{
		ID: overlayID, Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": baseID.String()}),
	}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: overlay}})
	require.NoError(t, err)

	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: base}})
	require.Error(t, err)
	durable := regapi.Entry{
		ID: regapi.NewID("app", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": overlayID.String()}),
	}
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.Error(t, err)
}

func TestOverlayCannotClaimIDAlreadyReferencedByDurableState(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	overlayID := regapi.NewID("runtime", "future")
	durable := regapi.Entry{
		ID: regapi.NewID("app", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": overlayID.String()}),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))
	candidate := regapi.Entry{ID: overlayID, Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: candidate}})
	require.Error(t, err)
}

func TestLoadStateClearsProcessLocalOverlays(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	entry := regapi.Entry{ID: regapi.NewID("runtime", "one"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	entries, generation, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.NotZero(t, generation)
	_, err = reg.GetEntry(entry.ID)
	require.Error(t, err)
}

func TestLoadStateInvalidatesAbsentOverlaySnapshot(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	_, staleGeneration, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, nil, v0))

	entry := regapi.Entry{ID: regapi.NewID("runtime", "stale"), Kind: regapi.EntryKind}
	_, err = reg.ApplyOverlay(ctx, "owner:a", staleGeneration, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
}

func TestDeletedOverlayOwnersDoNotRetainGenerationTombstones(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	entry := regapi.Entry{ID: regapi.NewID("runtime", "one"), Kind: regapi.EntryKind}
	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	deletedGeneration, err := reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: entry}})
	require.NoError(t, err)
	assert.Greater(t, deletedGeneration, generation)
	assert.Empty(t, reg.overlayGeneration)

	// Neither a snapshot from the active generation nor one opened before the
	// create/delete cycle may resurrect the removed owner.
	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.Error(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.Error(t, err)
}

func TestOverlayGenerationConflictIsStructuredAndRetryable(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	entry := regapi.Entry{ID: regapi.NewID("runtime", "one"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	assert.Equal(t, apierror.True, structured.Retryable())
	expected, ok := structured.Details().Get("expected_generation")
	require.True(t, ok)
	assert.Equal(t, uint64(1), expected)
	actual, ok := structured.Details().Get("actual_generation")
	require.True(t, ok)
	assert.Equal(t, uint64(0), actual)
}

func TestOverlayRejectsIDAliasAndManagedProvenance(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	base := regapi.Entry{ID: regapi.NewID("app", "base"), Kind: regapi.EntryKind}
	require.NoError(t, reg.LoadState(ctx, regapi.State{base}, version.FromParent(nil, regapi.RootVersion)))
	alias := regapi.Entry{ID: regapi.ID{Name: "app:base"}, Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: alias}})
	require.Error(t, err)
	managed := regapi.Entry{
		ID: regapi.NewID("runtime", "managed"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"module": "acme/app"}),
	}
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: managed}})
	require.Error(t, err)
}

func TestVersionSelectionRejectsHistoricalDurableDependencyOnOverlay(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	overlayID := regapi.NewID("runtime", "dep")
	consumer := regapi.Entry{
		ID: regapi.NewID("app", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": overlayID.String()}),
	}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: consumer}})
	require.NoError(t, err)
	v2, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: consumer}})
	require.NoError(t, err)
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{
		Kind:  regapi.EntryCreate,
		Entry: regapi.Entry{ID: overlayID, Kind: regapi.EntryKind},
	}})
	require.NoError(t, err)
	require.Error(t, reg.ApplyVersion(ctx, v1))
	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, v2.ID(), current.ID())
}

func TestVersionSelectionCannotRemoveDurableOverlayDependency(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	base := regapi.Entry{ID: regapi.NewID("app", "base"), Kind: regapi.EntryKind}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: base}})
	require.NoError(t, err)
	v2, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: base}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	overlay := regapi.Entry{
		ID: regapi.NewID("runtime", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{regapi.TagDependsOn: base.ID.String()}),
	}
	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: overlay}})
	require.NoError(t, err)
	require.Error(t, reg.ApplyVersion(ctx, v2))
	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, v1.ID(), current.ID())
}

func TestOverlayDependencyValidationUsesTopologySemantics(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		targetMeta attrs.Bag
		name       string
		dependency string
	}{
		{name: "relative ID", dependency: "target"},
		{name: "group", dependency: "group:targets", targetMeta: attrs.NewBagFrom(map[string]any{regapi.TagGroups: []any{"targets"}})},
		{name: "namespace", dependency: "ns:runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, _ := newOverlayTestRegistry(t)
			require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
			target := regapi.Entry{ID: regapi.NewID("runtime", "target"), Kind: regapi.EntryKind, Meta: tt.targetMeta}
			_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: target}})
			require.NoError(t, err)
			consumer := regapi.Entry{
				ID: regapi.NewID("runtime", "consumer"), Kind: regapi.EntryKind,
				Meta: attrs.NewBagFrom(map[string]any{"depends_on": tt.dependency}),
			}
			_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: consumer}})
			require.Error(t, err)
		})
	}
}

func TestOverlayExplicitDependenciesWorkWithoutResolver(t *testing.T) {
	ctx := context.Background()
	history := historymem.New()
	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	reg := NewRegistry(history, NewTestRunner(), builder, nil, zap.NewNop())
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	target := regapi.Entry{ID: regapi.NewID("runtime", "target"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: target}})
	require.NoError(t, err)
	consumer := regapi.Entry{
		ID: regapi.NewID("runtime", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{regapi.TagDependsOn: "target"}),
	}
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: consumer}})
	require.Error(t, err)
}

func TestOverlayAllowsDanglingBarePlaceholder(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	entry := regapi.Entry{
		ID: regapi.NewID("runtime", "source"), Kind: regapi.EntryKind,
		Data: payload.New(map[string]any{"token": "${API_KEY}"}),
	}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
}

func TestOverlayRejectsDuplicateOperations(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	entry := regapi.Entry{ID: regapi.NewID("runtime", "duplicate"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)
}

type failedCompensationRunner struct {
	base  TestRunner
	armed bool
	calls int
}

func (r *failedCompensationRunner) Transition(ctx context.Context, from regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
	if !r.armed {
		return r.base.Transition(ctx, from, changes)
	}
	r.calls++
	if r.calls > 1 {
		return from, errors.New("injected compensation failure")
	}
	partial, applyErr := r.base.Transition(ctx, from, changes)
	if applyErr != nil {
		partial = from
	}
	return partial, errors.New("injected transition failure")
}

func TestOverlayReconcilesIndexesAfterFailedCompensation(t *testing.T) {
	ctx := context.Background()
	history := historymem.New()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	runner := &failedCompensationRunner{}
	reg := NewRegistry(history, runner, builder, resolver, zap.NewNop())
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	runner.armed = true
	entry := regapi.Entry{ID: regapi.NewID("runtime", "partial"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.Error(t, err)
	assert.Equal(t, 2, runner.calls)
	_, err = reg.GetEntry(entry.ID)
	require.NoError(t, err)
	overlay, generation, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
	require.Len(t, overlay, 1)
	assert.Equal(t, entry.ID, overlay[0].ID)
}

func TestLoadStateReconcilesOverlaysAfterFailedCompensation(t *testing.T) {
	ctx := context.Background()
	history := historymem.New()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	runner := &failedCompensationRunner{}
	reg := NewRegistry(history, runner, builder, resolver, zap.NewNop())
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	entry := regapi.Entry{ID: regapi.NewID("runtime", "partial"), Kind: regapi.EntryKind}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	runner.armed = true
	regErr := reg.LoadState(ctx, nil, v0)
	require.Error(t, regErr)
	_, err = reg.GetEntry(entry.ID)
	require.Error(t, err)
	overlay, generation, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Empty(t, overlay)
	assert.Equal(t, uint64(2), generation)
}
