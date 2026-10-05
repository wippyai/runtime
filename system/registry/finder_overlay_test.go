// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/finder"
	"go.uber.org/zap"
)

func findIDs(t *testing.T, f regapi.Finder, query attrs.Bag) []regapi.ID {
	t.Helper()
	entries, err := f.Find(query)
	require.NoError(t, err)
	ids := make([]regapi.ID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func TestFinderObservesOverlayChanges(t *testing.T) {
	ctx := context.Background()
	reg, _ := newOverlayTestRegistry(t)
	require.NoError(t, reg.LoadState(ctx, nil, version.FromParent(nil, regapi.RootVersion)))
	f := finder.NewFinder(reg, zap.NewNop())
	query := attrs.Bag{"meta.type": "app"}

	require.Empty(t, findIDs(t, f, query))

	entry := regapi.Entry{ID: regapi.NewID("overlay.app", "main"), Kind: "test.resource",
		Meta: attrs.Bag{"type": "app", "title": "first"}, Data: payload.New("live")}
	generation, err := reg.ApplyOverlay(ctx, "owner:a", 0, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}})
	require.NoError(t, err)
	require.Equal(t, []regapi.ID{entry.ID}, findIDs(t, f, query))

	entry.Meta = attrs.Bag{"type": "app", "title": "second"}
	_, err = reg.ApplyOverlay(ctx, "owner:a", generation, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: entry}})
	require.NoError(t, err)
	found, err := f.Find(query)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "second", found[0].Meta["title"])
}
