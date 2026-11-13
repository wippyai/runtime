// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"fmt"

	"github.com/rqlite/go-sqlite3"
)

// NewDriver returns a pool-owned driver with Wippy's connection policy.
func NewDriver() *sqlite3.SQLiteDriver {
	return &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		// Commits never checkpoint inline: an automatic checkpoint runs on the
		// committing connection and copies WAL pages on that commit path.
		// The connector's background checkpointer owns checkpointing. This is
		// connection-local: a one-time pool Exec would not cover connection
		// recycling.
		if _, err := conn.Exec("PRAGMA wal_autocheckpoint=0", nil); err != nil {
			return fmt.Errorf("configure sqlite WAL autocheckpoint: %w", err)
		}
		return nil
	}}
}
