// SPDX-License-Identifier: MPL-2.0

package sql

import (
	dbsql "database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestPostgresTimestampCursorPrecision(t *testing.T) {
	// Reuse the PostgreSQL service provided by the registry integration job.
	dsn := strings.TrimSpace(os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN"))
	if dsn == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}
	db, err := dbsql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	const stamp = "2026-10-13T10:12:00.123456Z"
	const literal = "2026-10-13T10:12:00.123456789-04:00"
	var native time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT $1::timestamptz", stamp).Scan(&native))
	l := lua.NewState()
	defer l.Close()
	for _, tc := range []struct {
		handler interface {
			HandleResult(*lua.LState, any, error) []lua.LValue
		}
		name string
	}{
		{name: "query", handler: &QueryYield{}},
		{name: "prepared query", handler: &StmtQueryYield{}},
		{name: "transaction query", handler: &TxQueryYield{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			converted := tc.handler.HandleResult(l, sqlapi.QueryResponse{
				Columns: []string{"created_at", "literal", "missing"}, Rows: [][]any{{native, literal, nil}},
			}, nil)
			require.Len(t, converted, 2)
			require.Equal(t, lua.LNil, converted[1])
			row := converted[0].(*lua.LTable).RawGetInt(1).(*lua.LTable)
			cursor := row.RawGetString("created_at").String()
			require.Equal(t, literal, row.RawGetString("literal").String(), "text columns must remain byte-for-byte unchanged")
			require.Equal(t, lua.LNil, row.RawGetString("missing"))
			// Three rows within one second make loss of cursor precision
			// observable: truncation incorrectly selects the earlier row too.
			rows, err := db.QueryContext(t.Context(), `
SELECT id FROM (VALUES
 (1, '2026-10-13T10:12:00.123455Z'::timestamptz),
 (2, '2026-10-13T10:12:00.123456Z'::timestamptz),
 (3, '2026-10-13T10:12:00.123457Z'::timestamptz)
) AS messages(id, created_at)
WHERE created_at >= $1::timestamptz ORDER BY created_at, id`, cursor)
			require.NoError(t, err)
			defer rows.Close()
			var ids []int
			for rows.Next() {
				var id int
				require.NoError(t, rows.Scan(&id))
				ids = append(ids, id)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, []int{2, 3}, ids, "a Lua timestamp cursor must retain its exact database boundary")
			require.Equal(t, stamp, cursor)
		})
	}
}
