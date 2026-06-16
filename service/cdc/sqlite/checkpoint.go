// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
)

const createOffsetsSQL = `CREATE TABLE IF NOT EXISTS ` + offsetsTable + ` (
	source        TEXT PRIMARY KEY,
	last_seq      INTEGER NOT NULL DEFAULT 0,
	snapshot_done INTEGER NOT NULL DEFAULT 0,
	updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
)`

func ensureOffsets(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, createOffsetsSQL)
	return err
}

func loadOffset(ctx context.Context, db *sql.DB, source string) (snapshotDone bool, lastSeq uint64, err error) {
	row := db.QueryRowContext(ctx, "SELECT snapshot_done, last_seq FROM "+offsetsTable+" WHERE source = ?", source)
	var done int
	var seq int64
	switch scanErr := row.Scan(&done, &seq); scanErr {
	case nil:
		return done != 0, uint64(seq), nil
	case sql.ErrNoRows:
		return false, 0, nil
	default:
		return false, 0, scanErr
	}
}

func saveSnapshotDone(ctx context.Context, db *sql.DB, source string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO "+offsetsTable+" (source, snapshot_done, updated_at) VALUES (?, 1, datetime('now')) "+
			"ON CONFLICT(source) DO UPDATE SET snapshot_done = 1, updated_at = datetime('now')",
		source)
	return err
}

func saveOffset(ctx context.Context, db *sql.DB, source string, seq uint64) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		"INSERT INTO "+offsetsTable+" (source, last_seq, updated_at) VALUES (?, ?, datetime('now')) "+
			"ON CONFLICT(source) DO UPDATE SET last_seq = MAX(last_seq, excluded.last_seq), updated_at = datetime('now')",
		source, int64(seq))
	return err
}

func deleteOffset(ctx context.Context, db *sql.DB, source string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM "+offsetsTable+" WHERE source = ?", source)
	return err
}
