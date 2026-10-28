// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/internal/sqliteconn"
)

type pageQueryCounter struct {
	*sql.DB
	arguments []int
	queries   int
}

func (q *pageQueryCounter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.queries++
	q.arguments = append(q.arguments, len(args))
	return q.DB.QueryContext(ctx, query, args...)
}

func TestReadPageBoundsBytesRowsAndAncestry(t *testing.T) {
	for _, test := range []struct {
		name   string
		size   int
		count  int
		sparse bool
	}{
		{name: "rows", size: 1, count: 3000},
		{name: "bytes", size: 2 << 20, count: 40, sparse: true},
		{name: "oversized_transaction", size: int(replayPagePolicy.Bytes) + 1, count: 2, sparse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := sqliteconn.Open("file:" + filepath.Join(t.TempDir(), "pages.db"))
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			_, err := db.ExecContext(t.Context(), "CREATE TABLE changesets(version_id INTEGER PRIMARY KEY,data BLOB NOT NULL)")
			require.NoError(t, err)
			tx, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			path := make([]ReplayVersion, test.count)
			for i := range path {
				id := uint(i + 1)
				if test.sparse {
					id = uint(2 * (test.count - i)) // Sparse and opposite numeric order.
				}
				path[i] = ReplayVersion{ID: id, Size: int64(test.size)}
				data := make([]byte, test.size)
				data[0] = byte(i % 251)
				_, err := tx.ExecContext(t.Context(), "INSERT INTO changesets VALUES (?,?)", id, data)
				require.NoError(t, err)
				if test.sparse {
					// Unrelated branch payloads must never enter the page.
					_, err = tx.ExecContext(t.Context(), "INSERT INTO changesets VALUES (?,?)", id-1, []byte("unrelated"))
					require.NoError(t, err)
				}
			}
			require.NoError(t, tx.Commit())
			q := &pageQueryCounter{DB: db}
			store := Store{Changesets: "changesets", Parameter: func(int) string { return "?" }}
			consumed, pages := 0, 0
			for consumed < len(path) {
				page, err := store.ReadPage(t.Context(), q, path[consumed:])
				require.NoError(t, err)
				require.NotEmpty(t, page)
				require.LessOrEqual(t, len(page), replayPagePolicy.Rows)
				bytes := 0
				for i, data := range page {
					require.Len(t, data, test.size)
					require.Equal(t, byte((consumed+i)%251), data[0])
					bytes += len(data)
				}
				if int64(bytes) > replayPagePolicy.Bytes {
					require.Len(t, page, 1, "an oversized transaction occupies its own page")
				}
				var found int
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT 1").Scan(&found), "the page cursor releases the single connection")
				consumed += len(page)
				pages++
			}
			require.Equal(t, pages, q.queries, "each bounded page uses one payload query")
			for _, arguments := range q.arguments {
				require.GreaterOrEqual(t, arguments, 2, "bound range values retain PostgreSQL binary result transfer")
			}
			if test.size < int(replayPagePolicy.Bytes) {
				require.Less(t, pages, test.count)
			}
		})
	}
}

func TestReadPageRejectsChangedOrMissingPayload(t *testing.T) {
	db := sqliteconn.Open("file:" + filepath.Join(t.TempDir(), "invalid.db"))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err := db.ExecContext(t.Context(), "CREATE TABLE changesets(version_id INTEGER PRIMARY KEY,data BLOB NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO changesets VALUES (1,?)", []byte{0x90})
	require.NoError(t, err)
	store := Store{Changesets: "changesets", Parameter: func(i int) string { return fmt.Sprintf("?%d", i) }}
	_, err = store.ReadPage(t.Context(), db, []ReplayVersion{{ID: 1, Size: 2}})
	require.ErrorContains(t, err, "size changed")
	_, err = store.ReadPage(t.Context(), db, []ReplayVersion{{ID: 2, Size: 1}})
	require.ErrorContains(t, err, "changeset not found for version 2")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = store.ReadPage(ctx, db, []ReplayVersion{{ID: 1, Size: 1}})
	require.ErrorIs(t, err, context.Canceled)
}
