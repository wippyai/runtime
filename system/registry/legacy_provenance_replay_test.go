// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historypostgres "github.com/wippyai/runtime/system/registry/history/postgres"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type legacyReplayPayload struct {
	Data   any
	Format payload.Format
}

type legacyReplayEntry struct {
	Meta           attrs.Bag            `codec:"Meta"`
	Data           *legacyReplayPayload `codec:"Data"`
	ID             regapi.ID            `codec:"ID"`
	Kind           string               `codec:"Kind"`
	DependencyRoot bool                 `codec:"DependencyRoot,omitempty"`
}

type legacyReplayOperation struct {
	OriginalEntry *legacyReplayEntry `codec:"OriginalEntry"`
	Kind          string             `codec:"Kind"`
	Entry         legacyReplayEntry  `codec:"Entry"`
}

func encodeLegacyReplay(t *testing.T, op legacyReplayOperation) []byte {
	t.Helper()
	handle := &codec.MsgpackHandle{}
	handle.MapType = reflect.TypeOf(map[string]any(nil))
	handle.RawToString = true
	handle.Canonical = true
	var encoded bytes.Buffer
	require.NoError(t, codec.NewEncoder(&encoded, handle).Encode([]legacyReplayOperation{op}))
	return encoded.Bytes()
}

func legacyDependencyRootReplay(t *testing.T) (regapi.ProvenancedState, regapi.ID, regapi.Version, []byte) {
	t.Helper()
	dependencyID := regapi.NewID("kickside.knowledge.requirements", "kb10")
	entry := regapi.Entry{
		ID:   dependencyID,
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{
			"component": "kickside/kb10",
			"version":   "1.0.0",
		}),
	}
	baseline := regapi.ProvenancedState{
		Entries: regapi.State{entry},
		Provenance: regapi.ProvenanceMap{
			dependencyID: {Module: "kickside/knowledge", Version: "1.0.0", Digest: "sha256:knowledge"},
		},
	}
	v1 := version.FromParent(version.New(regapi.RootVersion), 1)
	legacyRow := encodeLegacyReplay(t, legacyReplayOperation{
		Kind: regapi.EntryUpdate,
		Entry: legacyReplayEntry{
			ID:             entry.ID,
			Kind:           entry.Kind,
			Data:           &legacyReplayPayload{Data: entry.Data.Data(), Format: entry.Data.Format()},
			DependencyRoot: true,
		},
	})
	return baseline, dependencyID, v1, legacyRow
}

func assertLegacyDependencyRootReplay(
	ctx context.Context,
	t *testing.T,
	history regapi.History,
	baseline regapi.ProvenancedState,
	dependencyID regapi.ID,
	v1 regapi.Version,
) {
	t.Helper()
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
	require.Equal(t, []regapi.ID{dependencyID}, reg.DependencyRoots(),
		"every cold replay must preserve the legacy application root")
	record, ok := reg.EntryProvenance(dependencyID)
	require.True(t, ok)
	require.True(t, record.Root)
	require.Equal(t, "kickside/knowledge", record.Module,
		"root promotion must not replace module ownership")
}

// TestLoadStatePromotesLegacyDependencyRoot exercises the pre-provenance wire
// boundary. The history operation deliberately has no provenance record: its
// Entry.DependencyRoot flag is the only durable statement that the dependency
// was promoted to an application root.
func TestLoadStatePromotesLegacyDependencyRoot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "registry.db")
	baseline, dependencyID, v1, legacyRow := legacyDependencyRootReplay(t)

	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, history.Save(v1, nil, true))
	require.NoError(t, history.Close())

	// Replace the modern empty row with the byte shape emitted before
	// provenance existed. Going through today's Save would intentionally strip
	// DependencyRoot and would not exercise an upgrade at all.
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE changesets SET data = ? WHERE version_id = ?", legacyRow, v1.ID())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	for i := 0; i < 2; i++ {
		history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
		require.NoError(t, err)
		assertLegacyDependencyRootReplay(ctx, t, history, baseline, dependencyID, v1)
		require.NoError(t, history.Close())
	}
}

func TestLoadStatePromotesLegacyDependencyRootPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN"))
	if dsn == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("wippy_registry_legacy_root_%d", os.Getpid())
	baseline, dependencyID, v1, legacyRow := legacyDependencyRootReplay(t)

	history, err := historypostgres.NewPostgres(dsn, schema, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, history.Save(v1, nil, true))
	require.NoError(t, history.Close())

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
		_ = db.Close()
	})
	_, err = db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %q.changesets SET data = $1 WHERE version_id = $2", schema), legacyRow, v1.ID())
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		history, err := historypostgres.NewPostgres(dsn, schema, zap.NewNop())
		require.NoError(t, err)
		assertLegacyDependencyRootReplay(ctx, t, history, baseline, dependencyID, v1)
		require.NoError(t, history.Close())
	}
}
