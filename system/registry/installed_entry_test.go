// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
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
