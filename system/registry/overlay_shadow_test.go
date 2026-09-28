// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
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

func shadowTestEntry(name, data string) regapi.Entry {
	return regapi.Entry{ID: regapi.NewID("app", name), Kind: regapi.EntryKind, Data: payload.New(data)}
}

// Invariant 1: a shadow update replaces the durable entry in effective state
// and reaches the runner as an ordinary update.
func TestOverlayShadowUpdateReplacesDurableEntry(t *testing.T) {
	ctx := context.Background()
	reg, _, runner := newOverlayTestRegistryWithRunner(t)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	shadow := shadowTestEntry("setting", "shadowed")
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadow}})
	require.NoError(t, err)

	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "shadowed", stored.Data.Data())

	last := runner.LastTransition()
	require.Len(t, last, 1)
	assert.Equal(t, regapi.EntryUpdate, last[0].Kind)
	assert.Equal(t, durable.ID, last[0].Entry.ID)
	assert.Equal(t, "shadowed", last[0].Entry.Data.Data())
}

// Invariant 1: a shadow delete removes the durable entry from effective state
// and reaches the runner as an ordinary delete.
func TestOverlayShadowDeleteRemovesDurableEntry(t *testing.T) {
	ctx := context.Background()
	reg, _, runner := newOverlayTestRegistryWithRunner(t)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)

	_, err = reg.GetEntry(durable.ID)
	require.Error(t, err)
	last := runner.LastTransition()
	require.Len(t, last, 1)
	assert.Equal(t, regapi.EntryDelete, last[0].Kind)
	assert.Equal(t, durable.ID, last[0].Entry.ID)
}

// Invariant 2: the shadow owner is recorded, foreign owners are refused, and
// the shadow owner may keep mutating its own shadow.
func TestOverlayShadowIsOwnedByOneOverlay(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "a")}})
	require.NoError(t, err)
	assert.Equal(t, "owner:a", reg.overlayOwners[durable.ID])

	_, err = reg.ApplyOverlay(ctx, "owner:b", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "b")}})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.Error(t, err)

	generation, err = reg.ApplyOverlay(ctx, "owner:a", generation,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "a2")}})
	require.NoError(t, err)
	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "a2", stored.Data.Data())

	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
}

// Invariant 3: releasing a shadow update returns the durable entry to
// effective state through an ordinary restoring transition.
func TestOverlayShadowReleaseRestoresDurableEntry(t *testing.T) {
	ctx := context.Background()
	reg, _, runner := newOverlayTestRegistryWithRunner(t)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "shadowed")}})
	require.NoError(t, err)

	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)

	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "durable", stored.Data.Data())
	last := runner.LastTransition()
	require.Len(t, last, 1)
	assert.Equal(t, regapi.EntryUpdate, last[0].Kind)
	assert.Equal(t, "durable", last[0].Entry.Data.Data())

	entries, _, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Empty(t, entries)
	_, claimed := reg.overlayOwners[durable.ID]
	assert.False(t, claimed)
}

// Invariant 3: releasing a shadow delete recreates the durable entry.
func TestOverlayShadowDeleteReleaseRestoresDurableEntry(t *testing.T) {
	ctx := context.Background()
	reg, _, runner := newOverlayTestRegistryWithRunner(t)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
	_, err = reg.GetEntry(durable.ID)
	require.Error(t, err)

	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)

	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "durable", stored.Data.Data())
	last := runner.LastTransition()
	require.Len(t, last, 1)
	assert.Equal(t, regapi.EntryCreate, last[0].Kind)
	assert.Equal(t, "durable", last[0].Entry.Data.Data())
}

// Invariant 3: the restored content is the durable entry of the selected
// version, not the content the shadow originally displaced.
func TestOverlayShadowRestoresSelectedDurableVersion(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	durable := shadowTestEntry("setting", "v1")
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "v2")}})
	require.NoError(t, err)

	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "shadowed")}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "shadowed", stored.Data.Data())

	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
	stored, err = reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "v1", stored.Data.Data())
}

// Invariant 4: durable entries may depend on a shadowed durable entry, which
// is still resident, while an overlay-created entry stays off limits.
func TestDurableEntryMayDependOnShadowedEntry(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	target := shadowTestEntry("target", "durable")
	consumer := regapi.Entry{
		ID: regapi.NewID("app", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": target.ID.String()}),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{target, consumer}, version.FromParent(nil, regapi.RootVersion)))

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("target", "shadowed")}})
	require.NoError(t, err)
	stored, err := reg.GetEntry(target.ID)
	require.NoError(t, err)
	assert.Equal(t, "shadowed", stored.Data.Data())
}

// Invariant 4: a shadow delete may not remove an entry a live entry depends on.
func TestOverlayShadowDeleteRefusedWhileDependentSurvives(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	target := shadowTestEntry("target", "durable")
	consumer := regapi.Entry{
		ID: regapi.NewID("app", "consumer"), Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{"depends_on": target.ID.String()}),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{target, consumer}, version.FromParent(nil, regapi.RootVersion)))

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: target}})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	_, err = reg.GetEntry(target.ID)
	require.NoError(t, err)
}

// Invariant 5: every durable write path refuses a shadowed entry and applies
// nothing.
func TestDurableApplyRefusedWhileEntryIsShadowed(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	durable := shadowTestEntry("setting", "durable")
	base, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)
	plan, err := reg.Plan(ctx, base, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "planned")}})
	require.NoError(t, err)

	_, err = reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "shadowed")}})
	require.NoError(t, err)

	for name, apply := range map[string]func() error{
		"Apply": func() error {
			_, applyErr := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "durable2")}})
			return applyErr
		},
		"ApplyAt": func() error {
			_, applyErr := reg.ApplyAt(ctx, base, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
			return applyErr
		},
		"ApplyPlan": func() error {
			_, applyErr := reg.ApplyPlan(ctx, plan)
			return applyErr
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := apply()
			require.Error(t, err)
			var structured apierror.Error
			require.ErrorAs(t, err, &structured)
			assert.Equal(t, apierror.Conflict, structured.Kind())
			stored, getErr := reg.GetEntry(durable.ID)
			require.NoError(t, getErr)
			assert.Equal(t, "shadowed", stored.Data.Data())
			current, currentErr := reg.Current()
			require.NoError(t, currentErr)
			assert.Equal(t, base.ID(), current.ID())
		})
	}
}

// Invariant 6: shadows stay out of history and out of a cold boot, and belong
// to their owner's overlay state.
func TestOverlayShadowStaysProcessLocal(t *testing.T) {
	ctx := context.Background()
	reg, history := newOverlayTestRegistry(t)
	v0 := version.FromParent(nil, regapi.RootVersion)
	durable := shadowTestEntry("setting", "durable")
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, v0))

	_, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "shadowed")}})
	require.NoError(t, err)

	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(0), current.ID())
	head, err := history.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(0), head.ID())
	entries, _, err := reg.GetOverlay("owner:a")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "shadowed", entries[0].Data.Data())

	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, v0))
	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "durable", stored.Data.Data())
	assert.Empty(t, reg.overlayShadows)
	entries, _, err = reg.GetOverlay("owner:a")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// Invariant 6: a shadow delete stays claimed by its owner across durable
// applies of unrelated entries.
func TestOverlayShadowDeleteRetainsOwnershipClaim(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	durable := shadowTestEntry("setting", "durable")
	_, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)

	_, err = reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: shadowTestEntry("other", "durable")}})
	require.NoError(t, err)

	assert.Equal(t, "owner:a", reg.overlayOwners[durable.ID])
	_, err = reg.ApplyOverlay(ctx, "owner:b", 0, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "b")}})
	require.Error(t, err)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "durable2")}})
	require.Error(t, err)
}

// Invariant 3: a shadow claims a durable entry, so a version that does not
// carry that entry is refused rather than silently reinterpreted.
func TestVersionSelectionRefusedWhileEntryShadowedIsAbsent(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	durable := shadowTestEntry("setting", "v1")
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: durable}})
	require.NoError(t, err)
	v2, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))

	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0,
		regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadowTestEntry("setting", "shadowed")}})
	require.NoError(t, err)

	require.Error(t, reg.ApplyVersion(ctx, v2))
	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, v1.ID(), current.ID())
	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "shadowed", stored.Data.Data())
	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: durable}})
	require.NoError(t, err)
}

// Invariant 7: directive-owned kinds stay out of overlays, shadows included.
func TestOverlayShadowRejectsDirectiveOwnedKinds(t *testing.T) {
	ctx := context.Background()
	history := historymem.New()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	reg := NewRegistry(history, NewTestRunner(), builder, resolver, zap.NewNop(),
		WithKindDirective("expanded.kind", overlayTestDirective{}))
	durable := regapi.Entry{ID: regapi.NewID("app", "declaration"), Kind: "expanded.kind", Data: payload.New("durable")}
	require.NoError(t, reg.LoadState(ctx, regapi.State{durable}, version.FromParent(nil, regapi.RootVersion)))

	shadow := regapi.Entry{ID: durable.ID, Kind: "expanded.kind", Data: payload.New("shadowed")}
	_, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: shadow}})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Invalid, structured.Kind())
	stored, err := reg.GetEntry(durable.ID)
	require.NoError(t, err)
	assert.Equal(t, "durable", stored.Data.Data())
}
