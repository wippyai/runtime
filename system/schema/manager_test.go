// SPDX-License-Identifier: MPL-2.0

package schema

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerSetupCreatesVersionTablesAndInitialSchema(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	bundle := Bundle{
		Name:           "test",
		InitialVersion: "1.0",
		SchemaPath:     "sqlite/test/schema.sql",
		FS: fstest.MapFS{
			"sqlite/test/schema.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE things (
	id TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`)},
		},
	}

	manager, err := NewManager(bundle, Target{
		Dialect:     DialectSQLite,
		DB:          db,
		LogicalName: "test_schema",
	})
	require.NoError(t, err)

	require.NoError(t, manager.Setup(context.Background()))

	version, err := manager.CurrentVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.0", version)

	_, err = db.ExecContext(context.Background(), "INSERT INTO things (id, value) VALUES ('a', 'b')")
	require.NoError(t, err)
}

func TestManagerUpdateAppliesVersionedManifestsInOrder(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	bundle := Bundle{
		Name:           "test",
		InitialVersion: "1.0",
		SchemaPath:     "sqlite/test/schema.sql",
		VersionedPath:  "sqlite/test/versioned",
		FS: fstest.MapFS{
			"sqlite/test/schema.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE things (
	id TEXT PRIMARY KEY
);
`)},
			"sqlite/test/versioned/v1.1/manifest.json": &fstest.MapFile{Data: []byte(`{
  "curr_version": "1.1",
  "min_compatible_version": "1.0",
  "description": "add value",
  "schema_update_files": ["add_value.sql"]
}`)},
			"sqlite/test/versioned/v1.1/add_value.sql": &fstest.MapFile{Data: []byte(`
ALTER TABLE things ADD COLUMN value TEXT;
`)},
			"sqlite/test/versioned/v1.2/manifest.json": &fstest.MapFile{Data: []byte(`{
  "curr_version": "1.2",
  "min_compatible_version": "1.1",
  "description": "add flag",
  "schema_update_files": ["add_flag.sql"]
}`)},
			"sqlite/test/versioned/v1.2/add_flag.sql": &fstest.MapFile{Data: []byte(`
ALTER TABLE things ADD COLUMN flag INTEGER NOT NULL DEFAULT 0;
`)},
		},
	}

	manager, err := NewManager(bundle, Target{
		Dialect:     DialectSQLite,
		DB:          db,
		LogicalName: "test_schema",
	})
	require.NoError(t, err)
	require.NoError(t, manager.Setup(context.Background()))
	require.NoError(t, manager.Update(context.Background()))

	version, err := manager.CurrentVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.2", version)

	rows, err := db.QueryContext(context.Background(), "PRAGMA table_info(things)")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		require.NoError(t, rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk))
		columns[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, columns["value"])
	assert.True(t, columns["flag"])
}

func TestManagerRejectsPostgresTargetWithoutSchema(t *testing.T) {
	_, err := NewManager(Bundle{
		Name:           "test",
		InitialVersion: "1.0",
		SchemaPath:     "postgres/test/schema.sql",
		FS:             fstest.MapFS{"postgres/test/schema.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
	}, Target{
		Dialect:     DialectPostgres,
		DB:          &sql.DB{},
		LogicalName: "test_schema",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema name is required")
}

func TestManagerErrorsWhenManifestVersionDoesNotMatchDirectory(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	manager, err := NewManager(Bundle{
		Name:           "test",
		InitialVersion: "1.0",
		SchemaPath:     "sqlite/test/schema.sql",
		VersionedPath:  "sqlite/test/versioned",
		FS: fstest.MapFS{
			"sqlite/test/schema.sql": &fstest.MapFile{Data: []byte("CREATE TABLE things (id TEXT PRIMARY KEY);")},
			"sqlite/test/versioned/v1.1/manifest.json": &fstest.MapFile{Data: []byte(`{
  "curr_version": "1.2",
  "min_compatible_version": "1.0",
  "description": "bad manifest",
  "schema_update_files": []
}`)},
		},
	}, Target{
		Dialect:     DialectSQLite,
		DB:          db,
		LogicalName: "test_schema",
	})
	require.NoError(t, err)

	require.NoError(t, manager.Setup(context.Background()))
	err = manager.Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest version")
}

func TestManagerPostgresSQLUsesQualifiedSchemaToken(t *testing.T) {
	sqlText, err := renderSQL([]byte(`CREATE TABLE IF NOT EXISTS {{schema}}.things (id TEXT PRIMARY KEY);`), Target{
		Dialect:    DialectPostgres,
		SchemaName: "wippy_registry",
	})
	require.NoError(t, err)

	assert.Equal(t, `CREATE TABLE IF NOT EXISTS "wippy_registry".things (id TEXT PRIMARY KEY);`, sqlText)
}

func TestManagerDoesNotAcceptSchemaTokenForSQLite(t *testing.T) {
	_, err := renderSQL([]byte(`CREATE TABLE {{schema}}.things (id TEXT PRIMARY KEY);`), Target{
		Dialect: DialectSQLite,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "{{schema}}")
}

func TestBundleRequiresEmbeddedSchemaFile(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	manager, err := NewManager(Bundle{
		Name:           "test",
		InitialVersion: "1.0",
		SchemaPath:     "missing.sql",
		FS:             fstest.MapFS{},
	}, Target{
		Dialect:     DialectSQLite,
		DB:          db,
		LogicalName: "test_schema",
	})
	require.NoError(t, err)

	err = manager.Setup(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
