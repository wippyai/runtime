// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestStatementTimeoutCancelsPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("WIPPY_SQL_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("WIPPY_SQL_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(t.Context()))

	cases := map[string]string{
		"db query": `
			local rows, err = db:query("SELECT pg_sleep(5)", nil, {timeout = "200ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "query")
			eq(err:retryable(), false, "retryable")`,
		"db execute": `
			local res, err = db:execute("SELECT pg_sleep(5)", nil, {timeout = "200ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "execute")`,
		"tx query": `
			local tx = check(db:begin())
			local rows, err = tx:query("SELECT pg_sleep(5)", nil, {timeout = "200ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "tx query")
			-- The driver closes a connection whose statement it canceled, which
			-- ends the transaction; rollback only releases it here.
			tx:rollback()`,
		"tx execute": `
			local tx = check(db:begin())
			local res, err = tx:execute("SELECT pg_sleep(5)", nil, {timeout = "200ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "tx execute")
			-- The driver closes a connection whose statement it canceled, which
			-- ends the transaction; rollback only releases it here.
			tx:rollback()`,
		"statement query": `
			local stmt = check(db:prepare("SELECT pg_sleep($1)"))
			local rows, err = stmt:query({5}, {timeout = "200ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "statement query")
			check(stmt:close())`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSQLEnv(t, db, sqlapi.Postgres)
			start := time.Now()
			e.run(script)
			require.Less(t, time.Since(start), 3*time.Second, "statement was not canceled promptly")
			e.run(`
				local rows, err = db:query("SELECT 1 AS n", nil, {timeout = "5s"})
				eq(err, nil, "database usable after timeout")
				eq(rows[1].n, 1, "value")`)
		})
	}
}
