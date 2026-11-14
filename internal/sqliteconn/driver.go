// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"errors"
	"fmt"

	"github.com/rqlite/go-sqlite3"
)

// NewDriver returns a pool-owned driver with Wippy's connection policy.
func NewDriver() *sqlite3.SQLiteDriver {
	return newDriver(defaultAutoCheckpointPages)
}

func checkpointBusy(err error) bool {
	var native sqlite3.Error
	return errors.As(err, &native) && (native.Code == sqlite3.ErrBusy || native.Code == sqlite3.ErrLocked)
}

func checkpointReadOnly(err error) bool {
	var native sqlite3.Error
	return errors.As(err, &native) && native.Code == sqlite3.ErrReadonly
}

func newDriver(autocheckpointPages int) *sqlite3.SQLiteDriver {
	policy := fmt.Sprintf("PRAGMA wal_autocheckpoint=%d", autocheckpointPages)
	return &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		// Apply the pool's policy to replacements too. Managed writable pools
		// use zero; other connections keep SQLite's normal policy, including
		// for writable databases attached to an in-memory or read-only main.
		if _, err := conn.Exec(policy, nil); err != nil {
			return fmt.Errorf("configure sqlite WAL autocheckpoint: %w", err)
		}
		return nil
	}}
}
