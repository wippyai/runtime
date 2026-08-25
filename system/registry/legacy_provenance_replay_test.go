// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// TestLoadStatePromotesLegacyDependencyRoot exercises the pre-provenance wire
// boundary. The history operation deliberately has no provenance record: its
// Entry.DependencyRoot flag is the only durable statement that the dependency
// was promoted to an application root.
func TestLoadStatePromotesLegacyDependencyRoot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "registry.db")
	dependencyID := regapi.NewID("kickside.knowledge.requirements", "kb10")
	baselineEntry := regapi.Entry{
		ID:   dependencyID,
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{
			"component": "kickside/kb10",
			"version":   "1.0.0",
		}),
	}
	baseline := regapi.ProvenancedState{
		Entries: regapi.State{baselineEntry},
		Provenance: regapi.ProvenanceMap{
			dependencyID: {Module: "kickside/knowledge", Version: "1.0.0", Digest: "sha256:knowledge"},
		},
	}
	legacyUpdate := baselineEntry
	legacyUpdate.DependencyRoot = true
	v0 := version.New(regapi.RootVersion)
	v1 := version.FromParent(v0, 1)

	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{
		Kind:  regapi.EntryUpdate,
		Entry: legacyUpdate,
	}}, true))
	require.NoError(t, history.Close())

	load := func() *Reg {
		t.Helper()
		history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, history.Close()) })

		stored, err := history.Get(v1)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.Nil(t, stored[0].Provenance, "fixture must remain a legacy operation")
		require.True(t, stored[0].Entry.DependencyRoot, "legacy wire flag is the only root statement")

		resolver := topology.NewResolver()
		reg := NewRegistry(
			history,
			NewTestRunner(),
			topology.NewStateBuilder(zap.NewNop(), resolver),
			resolver,
			zap.NewNop(),
		)
		require.NoError(t, reg.LoadState(ctx, baseline, v1))
		return reg
	}

	for i := 0; i < 2; i++ {
		reg := load()
		require.Equal(t, []regapi.ID{dependencyID}, reg.DependencyRoots(),
			"every cold replay must preserve the legacy application root")
		record, ok := reg.EntryProvenance(dependencyID)
		require.True(t, ok)
		require.True(t, record.Root)
		require.Equal(t, "kickside/knowledge", record.Module,
			"root promotion must not replace module ownership")
	}
}
