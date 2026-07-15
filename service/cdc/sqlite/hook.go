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

const sqliteCDCDriver = "sqlite3_wippy"

const (
	cdcInsert = sqlite3.SQLITE_INSERT
	cdcUpdate = sqlite3.SQLITE_UPDATE
	cdcDelete = sqlite3.SQLITE_DELETE
)

var (
	errNotSQLiteConn        = apierror.New(apierror.Invalid, "underlying connection is not a SQLite connection").WithRetryable(apierror.False)
	errCDCMemoryUnsupported = apierror.New(apierror.Invalid, "sqlite cdc requires a file-backed database").WithRetryable(apierror.False)
	errCaptureOwned         = apierror.New(apierror.Conflict, "sqlite cdc capture already owned for this database").WithRetryable(apierror.True)
)

type cdcSink interface {
	PreUpdate(op int, schema, table string, rowid int64, ncols int, old, new []any, scanErr error)
	Commit()
	Rollback()
}

type captureOwner struct {
	sink  cdcSink
	token uint64
}

var (
	cdcMu     sync.Mutex
	captures  = make(map[string]captureOwner)
	captureNo uint64
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

	cdcMu.Lock()
	defer cdcMu.Unlock()
	if owner, ok := captures[file]; ok {
		bindCDCHooks(conn, owner.sink)
	}

	return nil
}

func claimCapture(file string, sink cdcSink) (uint64, error) {
	cdcMu.Lock()
	defer cdcMu.Unlock()
	if _, ok := captures[file]; ok {
		return 0, errCaptureOwned
	}

	captureNo++
	captures[file] = captureOwner{sink: sink, token: captureNo}

	return captureNo, nil
}

func releaseCapture(file string, token uint64) {
	cdcMu.Lock()
	defer cdcMu.Unlock()
	if owner, ok := captures[file]; ok && owner.token == token {
		delete(captures, file)
	}
}

func installHooksOnRaw(raw any, sink cdcSink) (string, uint64, error) {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return "", 0, errNotSQLiteConn
	}

	file := normalizeCDCPath(conn.GetFilename("main"))
	if file == "" {
		return "", 0, errCDCMemoryUnsupported
	}

	token, err := claimCapture(file, sink)
	if err != nil {
		return file, 0, err
	}

	bindCDCHooks(conn, sink)

	return file, token, nil
}

func applyOwnerOnRaw(raw any, file string) error {
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		return errNotSQLiteConn
	}

	cdcMu.Lock()
	defer cdcMu.Unlock()
	if owner, ok := captures[file]; ok {
		bindCDCHooks(conn, owner.sink)
	} else {
		clearHooks(conn)
	}

	return nil
}

func clearHooks(conn *sqlite3.SQLiteConn) {
	conn.RegisterPreUpdateHook(nil)
	conn.RegisterCommitHook(nil)
	conn.RegisterRollbackHook(nil)
}

func normalizeCDCPath(path string) string {
	if path == "" {
		return ""
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
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
		var scanErr error
		switch d.Op {
		case sqlite3.SQLITE_INSERT:
			newRow, scanErr = scanPreUpdateRow(&d, count, true)
			rowid = d.NewRowID
		case sqlite3.SQLITE_DELETE:
			oldRow, scanErr = scanPreUpdateRow(&d, count, false)
			rowid = d.OldRowID
		case sqlite3.SQLITE_UPDATE:
			oldRow, scanErr = scanPreUpdateRow(&d, count, false)
			if scanErr == nil {
				newRow, scanErr = scanPreUpdateRow(&d, count, true)
			}
			rowid = d.NewRowID
		}

		sink.PreUpdate(d.Op, d.DatabaseName, d.TableName, rowid, count, oldRow, newRow, scanErr)
	})
	conn.RegisterCommitHook(func() int {
		sink.Commit()

		return 0
	})
	conn.RegisterRollbackHook(func() {
		sink.Rollback()
	})
}

func scanPreUpdateRow(d *sqlite3.SQLitePreUpdateData, count int, isNew bool) ([]any, error) {
	if count <= 0 {
		return nil, nil
	}

	vals := make([]any, count)
	var err error
	if isNew {
		err = d.New(vals...)
	} else {
		err = d.Old(vals...)
	}
	if err != nil {
		return nil, err
	}

	return vals, nil
}
