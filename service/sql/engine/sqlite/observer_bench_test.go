// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"testing"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Compare actual driver/commit work, not a mapper disconnected from capture.
// In-memory storage isolates observer CPU/allocation cost from disk latency.
func BenchmarkObserverCommit(b *testing.B) {
	for _, mode := range []string{"driver", "idle", "subscribed"} {
		b.Run(mode, func(b *testing.B) {
			var db *sql.DB
			var observer sqlapi.CommittedMutationSource
			var err error
			if mode == "driver" {
				db, err = sql.Open("sqlite3", ":memory:")
			} else {
				db, observer, err = openSQLite(context.Background(), ":memory:")
			}
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if observer != nil {
				defer observer.Close()
			}
			if _, err = db.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items VALUES(1,'value')`); err != nil {
				b.Fatal(err)
			}
			var stream sqlapi.MutationStream
			if mode == "subscribed" {
				stream, err = observer.Subscribe(context.Background(), sqlapi.MutationOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer stream.Close()
			}
			stmt, err := db.Prepare(`UPDATE items SET value=? WHERE id=1`)
			if err != nil {
				b.Fatal(err)
			}
			defer stmt.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = stmt.Exec("next"); err != nil {
					b.Fatal(err)
				}
				if stream != nil {
					if _, ok := <-stream.Changes(); !ok {
						b.Fatal(stream.Err())
					}
				}
			}
			b.StopTimer()
		})
	}
}
