// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func TestGetInstalledEntryFencesOverlaysAndRegistryInstances(t *testing.T) {
	ctx := t.Context()
	reg, _ := newOverlayTestRegistry(t)
	root := version.FromParent(nil, regapi.RootVersion)
	entry := regapi.Entry{
		ID:       regapi.NewID("app", "worker"),
		Kind:     regapi.EntryKind,
		Registry: regapi.EntryMetadata{Owner: "example/worker"},
		Data:     payload.New("installed"),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{entry}, root))
	first, err := reg.GetInstalledEntry(entry.ID, "")
	require.NoError(t, err)
	require.Equal(t, root.ID(), first.Version)
	require.Equal(t, entry.Registry, first.Entry.Registry)
	require.NotEmpty(t, first.Revision)

	matched, err := reg.GetInstalledEntry(entry.ID, first.Revision)
	require.NoError(t, err)
	require.Equal(t, first, matched)

	other, _ := newOverlayTestRegistry(t)
	require.NoError(t, other.LoadState(ctx, regapi.State{entry}, root))
	_, err = other.GetInstalledEntry(entry.ID, first.Revision)
	require.ErrorIs(t, err, ErrInstalledEntryRevisionChanged)

	shadow := regapi.Entry{ID: entry.ID, Kind: entry.Kind, Data: payload.New("overlay")}
	onlyOverlay := regapi.Entry{ID: regapi.NewID("app", "overlay"), Kind: entry.Kind}
	generation, err := reg.ApplyOverlay(ctx, "overlay", 0, regapi.ChangeSet{
		{Kind: regapi.EntryUpdate, Entry: shadow},
		{Kind: regapi.EntryCreate, Entry: onlyOverlay},
	})
	require.NoError(t, err)
	visible, err := reg.GetEntry(entry.ID)
	require.NoError(t, err)
	require.Equal(t, "overlay", visible.Data.Data())
	require.Equal(t, entry.Registry, visible.Registry)

	_, err = reg.GetInstalledEntry(entry.ID, first.Revision)
	require.ErrorIs(t, err, ErrInstalledEntryRevisionChanged)
	for _, id := range []regapi.ID{entry.ID, onlyOverlay.ID} {
		_, err = reg.GetInstalledEntry(id, "")
		var typed apierror.Error
		require.ErrorAs(t, err, &typed)
		require.Equal(t, apierror.NotFound, typed.Kind())
	}

	_, err = reg.ApplyOverlay(ctx, "overlay", generation, regapi.ChangeSet{
		{Kind: regapi.EntryDelete, Entry: shadow},
		{Kind: regapi.EntryDelete, Entry: onlyOverlay},
	})
	require.NoError(t, err)
	restored, err := reg.GetInstalledEntry(entry.ID, "")
	require.NoError(t, err)
	require.Equal(t, first.Version, restored.Version)
	require.NotEqual(t, first.Revision, restored.Revision)
	require.Equal(t, "installed", restored.Entry.Data.Data())

	update := entry
	update.Data = payload.New("new installed")
	next, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: update}})
	require.NoError(t, err)
	_, err = reg.GetInstalledEntry(entry.ID, restored.Revision)
	require.ErrorIs(t, err, ErrInstalledEntryRevisionChanged)
	updated, err := reg.GetInstalledEntry(entry.ID, "")
	require.NoError(t, err)
	require.Equal(t, next.ID(), updated.Version)
	require.Equal(t, entry.Registry, updated.Entry.Registry)
	require.Equal(t, "new installed", updated.Entry.Data.Data())
}

type installedEntryTestHistory struct {
	*historymem.Storage
	failSave bool
}

func (h *installedEntryTestHistory) Save(v regapi.Version, changes regapi.ChangeSet, head bool) error {
	if h.failSave {
		return errors.New("history save failed")
	}
	return h.Storage.Save(v, changes, head)
}

func TestGetInstalledEntryRefusesPartialStateUntilRecovery(t *testing.T) {
	ctx := t.Context()
	history := &installedEntryTestHistory{Storage: historymem.New()}
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	runner := newApplyingMockRunner(builder)
	reg := NewRegistry(history, runner, builder, resolver, zap.NewNop())
	root := version.New(regapi.RootVersion)
	entry := regapi.Entry{
		ID:       regapi.NewID("app", "worker"),
		Kind:     regapi.EntryKind,
		Data:     payload.New("installed"),
		Registry: regapi.EntryMetadata{Owner: "example/worker"},
	}
	baseline := regapi.State{entry}
	require.NoError(t, reg.LoadState(ctx, baseline, root))
	before, err := reg.GetInstalledEntry(entry.ID, "")
	require.NoError(t, err)

	history.failSave = true
	applyChanges := runner.RunFunc
	calls := 0
	runner.RunFunc = func(state regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
		calls++
		if calls == 1 {
			return applyChanges(state, changes)
		}
		return state, errors.New("compensation failed")
	}
	update := entry
	update.Data = payload.New("uncommitted")
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: update}})
	require.ErrorContains(t, err, "failed to save new version")
	require.Equal(t, 2, calls)
	observed, err := reg.GetEntry(entry.ID)
	require.NoError(t, err)
	require.Equal(t, "uncommitted", observed.Data.Data())
	require.Equal(t, entry.Registry, observed.Registry)
	current, err := reg.Current()
	require.NoError(t, err)
	require.Equal(t, before.Version, current.ID())

	for _, revision := range []string{"", before.Revision} {
		installed, readErr := reg.GetInstalledEntry(entry.ID, revision)
		require.Zero(t, installed)
		var typed apierror.Error
		require.ErrorAs(t, readErr, &typed)
		require.Equal(t, apierror.Unavailable, typed.Kind())
		require.Equal(t, apierror.True, typed.Retryable())
	}

	// A successful incremental write cannot reconcile the prior partial state.
	history.failSave = false
	runner.RunFunc = applyChanges
	unrelated := regapi.Entry{ID: regapi.NewID("app", "unrelated"), Kind: regapi.EntryKind}
	next, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: unrelated}})
	require.NoError(t, err)
	_, err = reg.GetInstalledEntry(entry.ID, "")
	require.ErrorContains(t, err, "installed registry state is unavailable")

	runner.RunFunc = func(state regapi.State, _ regapi.ChangeSet) (regapi.State, error) {
		return state, errors.New("state reload failed")
	}
	require.ErrorContains(t, reg.LoadState(ctx, baseline, next), "state reload failed")
	_, err = reg.GetInstalledEntry(entry.ID, "")
	require.ErrorContains(t, err, "installed registry state is unavailable")

	runner.RunFunc = applyChanges
	require.NoError(t, reg.LoadState(ctx, baseline, next))
	recovered, err := reg.GetInstalledEntry(entry.ID, "")
	require.NoError(t, err)
	require.Equal(t, "installed", recovered.Entry.Data.Data())
	require.Equal(t, entry.Registry, recovered.Entry.Registry)
	require.Equal(t, next.ID(), recovered.Version)
	require.NotEqual(t, before.Revision, recovered.Revision)
	_, err = reg.GetInstalledEntry(entry.ID, before.Revision)
	require.ErrorIs(t, err, ErrInstalledEntryRevisionChanged)
	_, err = reg.GetInstalledEntry(unrelated.ID, recovered.Revision)
	require.NoError(t, err)
}
