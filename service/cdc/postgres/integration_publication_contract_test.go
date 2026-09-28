//go:build integration

// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestPublicationSnapshotRejectsRestrictedRowsAndColumns(t *testing.T) {
	_, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE snapshot_scope_test (id integer PRIMARY KEY, secret text)`)
	require.NoError(t, err)
	defer db.ExecContext(ctx, `DROP TABLE snapshot_scope_test`)
	for _, declaration := range []string{"snapshot_scope_test WHERE (id > 10)", "snapshot_scope_test (id)"} {
		_, err = db.ExecContext(ctx, `CREATE PUBLICATION snapshot_scope_pub FOR TABLE `+declaration)
		require.NoError(t, err)
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		_, err = publishedTables(ctx, conn, "snapshot_scope_pub")
		conn.Close()
		_, dropErr := db.ExecContext(ctx, `DROP PUBLICATION snapshot_scope_pub`)
		require.NoError(t, dropErr)
		require.ErrorIs(t, err, config.ErrUnsupported)
	}
}
