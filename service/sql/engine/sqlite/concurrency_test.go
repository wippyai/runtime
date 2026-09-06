// SPDX-License-Identifier: MPL-2.0
package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	config "github.com/wippyai/runtime/api/service/sql"
)

func TestApplicationReadWriteTransactionSerializesCompetingWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cfg := &config.SQLiteConfig{File: filepath.Join(t.TempDir(), "app.db"), Pool: config.PoolConfig{MaxOpen: 8, MaxIdle: 8, MaxLifetime: time.Hour}}
	opened, err := (engine{}).Open(ctx, cfg)
	require.NoError(t, err)
	defer opened.DB.Close()
	if opened.Observer != nil {
		defer opened.Observer.Close()
	}
	(engine{}).Tune(opened.DB, cfg)
	require.NoError(t, (engine{}).Prepare(ctx, opened.DB, cfg))
	_, err = opened.DB.ExecContext(ctx, "CREATE TABLE counts(id INTEGER PRIMARY KEY, n INTEGER); INSERT INTO counts VALUES(1,0)")
	require.NoError(t, err)
	tx, err := opened.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var n int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT n FROM counts WHERE id=1").Scan(&n))
	done := make(chan error, 1)
	go func() { _, err := opened.DB.ExecContext(ctx, "UPDATE counts SET n=n+1 WHERE id=1"); done <- err }()
	// The competing operation must enter database/sql's connection wait queue,
	// rather than commit against the first transaction's established read view.
	require.Eventually(t, func() bool { return opened.DB.Stats().WaitCount > 0 }, time.Second, time.Millisecond, "competing writer escaped application transaction serialization")
	_, err = tx.ExecContext(ctx, "UPDATE counts SET n=? WHERE id=1", n+1)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	require.NoError(t, opened.DB.QueryRowContext(ctx, "SELECT n FROM counts WHERE id=1").Scan(&n))
	require.Equal(t, 2, n)
}
