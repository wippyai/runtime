// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_vec

package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Exercise the same automatic extension registration as cmd/wippy, through
// the connector-owned physical driver, rather than only testing link success.
func TestDriverExtensions(t *testing.T) {
	sqlitevec.Auto()
	opened, err := (engine{}).Open(context.Background(), &sqlapi.SQLiteConfig{File: filepath.Join(t.TempDir(), "extensions.db")})
	require.NoError(t, err)
	defer opened.DB.Close()
	if opened.Observer != nil {
		defer opened.Observer.Close()
	}
	var version string
	require.NoError(t, opened.DB.QueryRow(`SELECT vec_version()`).Scan(&version))
	require.NotEmpty(t, version)
	var distance float64
	require.NoError(t, opened.DB.QueryRow(`SELECT vec_distance_L2('[0,0]', '[3,4]')`).Scan(&distance))
	require.Equal(t, float64(5), distance)
	_, err = opened.DB.Exec(`CREATE VIRTUAL TABLE docs USING fts5(body); INSERT INTO docs VALUES('wippy runtime')`)
	require.NoError(t, err)
	var count int
	require.NoError(t, opened.DB.QueryRow(`SELECT count(*) FROM docs WHERE docs MATCH 'wippy'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, opened.DB.QueryRow(`SELECT json_extract('{"count":7}', '$.count')`).Scan(&count))
	require.Equal(t, 7, count)
}
