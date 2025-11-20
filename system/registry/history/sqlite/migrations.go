package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const currentSchemaVersion = 1

var migrations = []migration{
	{
		version: 1,
		up: `
CREATE TABLE IF NOT EXISTS metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS versions (
	id INTEGER PRIMARY KEY,
	parent_id INTEGER,
	FOREIGN KEY (parent_id) REFERENCES versions(id)
);

CREATE TABLE IF NOT EXISTS changesets (
	version_id INTEGER PRIMARY KEY,
	data BLOB NOT NULL,
	FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_versions_parent ON versions(parent_id);
`,
	},
}

type migration struct {
	version int
	up      string
}

func runMigrations(db *sql.DB) error {
	ctx := context.Background()
	var schemaVersion int

	var tableExists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='metadata')").Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check metadata table: %w", err)
	}

	if tableExists {
		err = db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'schema_version'").Scan(&schemaVersion)
		if errors.Is(err, sql.ErrNoRows) {
			schemaVersion = 0
		} else if err != nil {
			return fmt.Errorf("failed to read schema version: %w", err)
		}
	} else {
		schemaVersion = 0
	}

	if schemaVersion > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", schemaVersion, currentSchemaVersion)
	}

	for _, m := range migrations {
		if m.version <= schemaVersion {
			continue
		}

		if _, err := db.ExecContext(ctx, m.up); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", m.version, err)
		}

		if _, err := db.ExecContext(ctx, "INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', ?)", m.version); err != nil {
			return fmt.Errorf("failed to update schema version to %d: %w", m.version, err)
		}

		schemaVersion = m.version
	}

	return nil
}
