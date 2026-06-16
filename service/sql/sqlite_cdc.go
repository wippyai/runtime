// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sql

import (
	"database/sql"
	"path/filepath"
	"sync"

	"github.com/mattn/go-sqlite3"
)

const sqliteCDCDriver = "sqlite3_wippy"

const (
	CDCInsert = sqlite3.SQLITE_INSERT
	CDCUpdate = sqlite3.SQLITE_UPDATE
	CDCDelete = sqlite3.SQLITE_DELETE
)

type CDCSink interface {
	PreUpdate(op int, table string, rowid int64, old, new []any)
	Commit()
	Rollback()
}

var (
	cdcMu    sync.RWMutex
	cdcSinks = make(map[string]CDCSink)
)

func init() {
	sql.Register(sqliteCDCDriver, &sqlite3.SQLiteDriver{ConnectHook: cdcConnectHook})
	sqliteDriverName = sqliteCDCDriver
}

func cdcConnectHook(conn *sqlite3.SQLiteConn) error {
	file := normalizeCDCPath(conn.GetFilename("main"))
	if file == "" {
		return nil
	}
	cdcMu.RLock()
	sink, ok := cdcSinks[file]
	cdcMu.RUnlock()
	if ok {
		bindCDCHooks(conn, sink)
	}
	return nil
}

func RegisterCDCSink(file string, sink CDCSink) {
	cdcMu.Lock()
	cdcSinks[file] = sink
	cdcMu.Unlock()
}

func UnregisterCDCSink(file string) {
	cdcMu.Lock()
	delete(cdcSinks, file)
	cdcMu.Unlock()
}

func InstallCDCHooksOnRaw(raw any, sink CDCSink) (string, error) {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return "", ErrNotSQLiteConn
	}
	file := normalizeCDCPath(conn.GetFilename("main"))
	if file == "" {
		return "", ErrCDCMemoryUnsupported
	}
	bindCDCHooks(conn, sink)
	return file, nil
}

func ClearCDCHooksOnRaw(raw any) error {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return ErrNotSQLiteConn
	}
	conn.RegisterPreUpdateHook(nil)
	conn.RegisterCommitHook(nil)
	conn.RegisterRollbackHook(nil)
	return nil
}

func normalizeCDCPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

func bindCDCHooks(conn *sqlite3.SQLiteConn, sink CDCSink) {
	conn.RegisterPreUpdateHook(func(d sqlite3.SQLitePreUpdateData) {
		count := d.Count()
		var oldRow, newRow []any
		var rowid int64
		switch d.Op {
		case sqlite3.SQLITE_INSERT:
			newRow = scanPreUpdateRow(&d, count, true)
			rowid = d.NewRowID
		case sqlite3.SQLITE_DELETE:
			oldRow = scanPreUpdateRow(&d, count, false)
			rowid = d.OldRowID
		case sqlite3.SQLITE_UPDATE:
			oldRow = scanPreUpdateRow(&d, count, false)
			newRow = scanPreUpdateRow(&d, count, true)
			rowid = d.NewRowID
		}
		sink.PreUpdate(d.Op, d.TableName, rowid, oldRow, newRow)
	})
	conn.RegisterCommitHook(func() int {
		sink.Commit()
		return 0
	})
	conn.RegisterRollbackHook(func() {
		sink.Rollback()
	})
}

func scanPreUpdateRow(d *sqlite3.SQLitePreUpdateData, count int, isNew bool) []any {
	if count <= 0 {
		return nil
	}
	vals := make([]any, count)
	if isNew {
		_ = d.New(vals...)
	} else {
		_ = d.Old(vals...)
	}
	return vals
}
