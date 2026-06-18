// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"path/filepath"
	"sync"

	"database/sql"

	"github.com/mattn/go-sqlite3"

	apierror "github.com/wippyai/runtime/api/error"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

// sqliteCDCDriver is a SQLite driver variant whose ConnectHook rebinds preupdate
// hooks on every connection the pool opens to a file with a registered sink.
const sqliteCDCDriver = "sqlite3_wippy"

const (
	cdcInsert = sqlite3.SQLITE_INSERT
	cdcUpdate = sqlite3.SQLITE_UPDATE
	cdcDelete = sqlite3.SQLITE_DELETE
)

var (
	errNotSQLiteConn        = apierror.New(apierror.Invalid, "underlying connection is not a SQLite connection").WithRetryable(apierror.False)
	errCDCMemoryUnsupported = apierror.New(apierror.Invalid, "sqlite cdc requires a file-backed database").WithRetryable(apierror.False)
)

// cdcSink receives row-level changes observed on the writer connection.
type cdcSink interface {
	PreUpdate(op int, table string, rowid int64, old, new []any)
	Commit()
	Rollback()
}

var (
	cdcMu    sync.RWMutex
	cdcSinks = make(map[string]cdcSink)
)

func init() {
	sql.Register(sqliteCDCDriver, &sqlite3.SQLiteDriver{ConnectHook: cdcConnectHook})
	sqlservice.RegisterDriver(sqlconfig.SQLite, sqliteCDCDriver)
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

func registerSink(file string, sink cdcSink) {
	cdcMu.Lock()
	cdcSinks[file] = sink
	cdcMu.Unlock()
}

func unregisterSink(file string) {
	cdcMu.Lock()
	delete(cdcSinks, file)
	cdcMu.Unlock()
}

func installHooksOnRaw(raw any, sink cdcSink) (string, error) {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return "", errNotSQLiteConn
	}
	file := normalizeCDCPath(conn.GetFilename("main"))
	if file == "" {
		return "", errCDCMemoryUnsupported
	}
	bindCDCHooks(conn, sink)
	return file, nil
}

func clearHooksOnRaw(raw any) error {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return errNotSQLiteConn
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

func bindCDCHooks(conn *sqlite3.SQLiteConn, sink cdcSink) {
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
