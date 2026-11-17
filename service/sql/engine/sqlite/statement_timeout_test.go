// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

type timeoutReceiver chan any

func (r timeoutReceiver) CompleteYield(_ uint64, data any, _ error) { r <- data }

// A statement timeout interrupts a statement on a managed file pool, and the
// pool keeps serving reads and writes afterwards.
func TestManagedPoolStatementTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	_, err = opened.DB.ExecContext(ctx, "CREATE TABLE counts(n INTEGER)")
	require.NoError(t, err)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	sqlservice.NewDispatcher().RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	dispatch := func(cmd dispatcher.Command) any {
		t.Helper()
		results := make(timeoutReceiver, 1)
		require.NoError(t, handlers[cmd.CmdID()].Handle(ctx, cmd, 1, results))
		select {
		case data := <-results:
			return data
		case <-ctx.Done():
			t.Fatal("dispatcher did not complete")
			return nil
		}
	}
	const endless = "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT max(x) FROM c"

	start := time.Now()
	query := dispatch(&config.QueryCmd{DB: opened.DB, Query: endless, Timeout: 100 * time.Millisecond}).(config.QueryResponse)
	require.ErrorIs(t, query.Error, context.DeadlineExceeded)
	write := dispatch(&config.ExecuteCmd{DB: opened.DB, Query: "INSERT INTO counts " + endless, Timeout: 100 * time.Millisecond}).(config.ExecuteResponse)
	require.ErrorIs(t, write.Error, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 3*time.Second)

	insert := dispatch(&config.ExecuteCmd{DB: opened.DB, Query: "INSERT INTO counts VALUES (1)", Timeout: time.Second}).(config.ExecuteResponse)
	require.NoError(t, insert.Error)
	rows := dispatch(&config.QueryCmd{DB: opened.DB, Query: "SELECT n FROM counts", Timeout: time.Second}).(config.QueryResponse)
	require.NoError(t, rows.Error)
	require.Equal(t, [][]any{{int64(1)}}, rows.Rows)
}
