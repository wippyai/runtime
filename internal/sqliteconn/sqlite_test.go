// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenPreservesSynchronization(t *testing.T) {
	for _, test := range []struct {
		name string
		dsn  string
		want int
	}{
		{name: "default", want: 1},
		{name: "full", dsn: "&_synchronous=FULL", want: 2},
		{name: "off", dsn: "&_synchronous=OFF", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := Open("file:" + filepath.Join(t.TempDir(), "app.db") + "?_journal_mode=WAL" + test.dsn)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			var synchronous, checkpoint int
			require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&synchronous))
			require.Equal(t, test.want, synchronous)
			require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
			require.Equal(t, 1000, checkpoint)
		})
	}
}

func TestOpenReadOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.db")
	ctx := context.Background()
	writer := Open("file:" + file + "?_journal_mode=WAL")
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	_, err := writer.ExecContext(ctx, "CREATE TABLE items (id INTEGER)")
	require.NoError(t, err)
	reader := Open("file:" + file + "?mode=ro")
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	reader.SetMaxOpenConns(1)
	// Force a replacement as well as the initial read-only connection.
	for i := 0; i < 2; i++ {
		var checkpoint int
		require.NoError(t, reader.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
		require.Equal(t, 1000, checkpoint)
		_, err = reader.ExecContext(ctx, "INSERT INTO items VALUES (1)")
		require.ErrorContains(t, err, "readonly")
		reader.SetMaxIdleConns(0)
		reader.SetMaxIdleConns(1)
	}
}
