// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/sqliteconn"
)

func TestPrepareProjectionReadsEachPayloadOnce(t *testing.T) {
	db := sqliteconn.Open("file:" + filepath.Join(t.TempDir(), "prepare.db"))
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, query := range []string{
		"CREATE TABLE versions(id INTEGER PRIMARY KEY,parent_id INTEGER,lineage_digest TEXT)",
		"CREATE TABLE changesets(version_id INTEGER PRIMARY KEY,data BLOB NOT NULL)",
		"CREATE TABLE replay_checkpoints(version_id INTEGER PRIMARY KEY,format_version INTEGER,digest TEXT,lineage_digest TEXT,data BLOB)",
	} {
		_, err := db.ExecContext(t.Context(), query)
		require.NoError(t, err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	const count = 1200
	var encodedBytes int
	for id := 0; id <= count; id++ {
		var parent any
		if id > 0 {
			parent = id - 1
		}
		_, err := tx.ExecContext(t.Context(), "INSERT INTO versions(id,parent_id) VALUES (?,?)", id, parent)
		require.NoError(t, err)
		data := []byte(fmt.Sprintf("payload-%d", id))
		_, err = tx.ExecContext(t.Context(), "INSERT INTO changesets VALUES (?,?)", id, data)
		require.NoError(t, err)
		if id > 0 {
			encodedBytes += len(data)
		}
	}
	require.NoError(t, tx.Commit())
	decoded, readBytes := 0, 0
	entry := registry.Entry{ID: registry.NewID("app", "setting"), Kind: "config"}
	store := Store{
		Versions: "versions", Changesets: "changesets", Checkpoints: "replay_checkpoints",
		Parameter: func(int) string { return "?" },
		Decode: func(data []byte) (registry.ChangeSet, error) {
			decoded++
			readBytes += len(data)
			return registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: entry}}, nil
		},
		EncodeState: func(state *registry.ReplayState) ([]byte, error) {
			require.Equal(t, uint64(count), state.Transaction)
			return []byte("encoded projection"), nil
		},
	}
	q := &pageQueryCounter{DB: db}
	state, prepared, err := store.Prepare(t.Context(), q, count)
	require.NoError(t, err)
	require.Equal(t, count, decoded)
	require.Equal(t, encodedBytes, readBytes, "preparation reads exactly one lineage")
	require.Equal(t, 3, q.queries, "one ancestry query and two bounded payload pages")
	require.Equal(t, entry, state.Entries[entry.ID])
	var before int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM replay_checkpoints").Scan(&before))
	require.Zero(t, before, "preparation leaves activation writes to the caller")

	// The projected state can be consumed independently of the encoded cache.
	delete(state.Entries, entry.ID)
	store.Decode = func([]byte) (registry.ChangeSet, error) { t.Fatal("activation rereads history"); return nil, nil }
	store.EncodeState = func(*registry.ReplayState) ([]byte, error) {
		t.Fatal("activation reconstructs projection")
		return nil, nil
	}
	tx, err = db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, store.WritePrepared(t.Context(), tx, prepared))
	require.NoError(t, tx.Commit())
	var data []byte
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT data FROM replay_checkpoints WHERE version_id=?", count).Scan(&data))
	require.Equal(t, []byte("encoded projection"), data)
	require.Equal(t, 3, q.queries)
	var chain sql.NullString
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT lineage_digest FROM versions WHERE id=?", count).Scan(&chain))
	require.True(t, chain.Valid)
}
