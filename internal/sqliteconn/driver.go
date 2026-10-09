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
		// rqlite's fork disables automatic checkpoints for its Raft snapshot
		// machinery. Wippy uses SQLite directly and retains SQLite's ordinary
		// 1000-page PASSIVE checkpoint trigger. This is connection-local: a
		// one-time pool Exec would not cover connection recycling.
		if _, err := conn.Exec("PRAGMA wal_autocheckpoint=1000", nil); err != nil {
			return fmt.Errorf("configure sqlite WAL autocheckpoint: %w", err)
		}
		return nil
	}}
}
