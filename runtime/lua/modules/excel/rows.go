// SPDX-License-Identifier: MPL-2.0

package excel

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/xuri/excelize/v2"
)

const rowsTypeName = "excel.Rows"

// maxReadBatch bounds the number of rows a single read(n) call can return.
const maxReadBatch = 10000

var rowsMetatable *lua.LTable

// Rows is a streaming cursor over the rows of a single sheet. It reads the
// sheet XML incrementally instead of materializing the whole sheet in memory.
// A cursor is owned by the workbook that created it: closing the workbook
// closes any cursors still open on it. Reads are pull-based and synchronous;
// once the cursor reaches end of sheet or fails, that terminal state is
// sticky for all subsequent read() calls.
type Rows struct {
	rows     *excelize.Rows
	wb       *Workbook
	err      error
	done     bool
	closed   bool
	released bool
}

// Close closes the cursor and unregisters it from its workbook. Idempotent.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.release()
}

// release closes the underlying iterator and unregisters it from its
// workbook. Unlike Close, it does not make a terminal EOF or error state
// unreadable, so subsequent reads can keep returning that sticky state.
func (r *Rows) release() error {
	if r.released {
		return nil
	}
	r.released = true

	err := r.rows.Close()
	if r.wb != nil && r.wb.cursors != nil {
		delete(r.wb.cursors, r)
	}
	return err
}

var rowsMethods = map[string]lua.LGoFunc{
	"read":  rowsRead,
	"close": rowsClose,
}

func checkRows(l *lua.LState, _ int) *Rows {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Rows); ok {
		return v
	}
	return nil
}

func workbookRows(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return invalidError(l, "workbook expected")
	}

	if wb.closed {
		return invalidError(l, "workbook is closed")
	}

	sheetName := l.CheckString(2)
	rows, err := wb.file.Rows(sheetName)
	if err != nil {
		return invalidWrapError(l, err, "open rows cursor")
	}

	r := &Rows{rows: rows, wb: wb}
	if wb.cursors == nil {
		wb.cursors = make(map[*Rows]struct{})
	}
	wb.cursors[r] = struct{}{}

	value.PushUserData(l, r, rowsMetatable)
	l.Push(lua.LNil)
	return 2
}

func rowsRead(l *lua.LState) int {
	r := checkRows(l, 1)
	if r == nil {
		return invalidError(l, "rows cursor expected")
	}

	if r.closed {
		return invalidError(l, "rows cursor is closed")
	}

	if r.err != nil {
		return internalError(l, r.err, "read rows")
	}

	if r.done {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	n := l.OptInt(2, 1)
	if n < 1 {
		return invalidError(l, "batch size must be positive")
	}
	if n > maxReadBatch {
		n = maxReadBatch
	}

	ctx := l.Context()
	batch := lua.CreateTable(n, 0)
	count := 0
	for count < n {
		if ctx != nil {
			select {
			case <-ctx.Done():
				r.err = ctx.Err()
				_ = r.release()
				return internalError(l, r.err, "read rows")
			default:
			}
		}

		if !r.rows.Next() {
			readErr := r.rows.Error()
			closeErr := r.release()
			if readErr != nil {
				r.err = readErr
				return internalError(l, readErr, "read rows")
			}
			if closeErr != nil {
				r.err = closeErr
				return internalError(l, closeErr, "close rows cursor")
			}
			r.done = true
			break
		}

		row, err := r.rows.Columns()
		if err != nil {
			r.err = err
			_ = r.release()
			return internalError(l, err, "read row")
		}

		luaRow := lua.CreateTable(len(row), 0)
		for colIdx, cellValue := range row {
			luaRow.RawSetInt(colIdx+1, lua.LString(cellValue))
		}
		count++
		batch.RawSetInt(count, luaRow)
	}

	if count == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(batch)
	l.Push(lua.LNil)
	return 2
}

func rowsClose(l *lua.LState) int {
	r := checkRows(l, 1)
	if r == nil {
		return singleErrorMsg(l, "rows cursor expected")
	}

	if err := r.Close(); err != nil {
		return singleError(l, err, "close rows cursor")
	}

	l.Push(lua.LNil)
	return 1
}

func rowsToString(l *lua.LState) int {
	r := checkRows(l, 1)
	if r == nil {
		return 0
	}

	if r.closed {
		l.Push(lua.LString("excel.Rows{closed}"))
	} else {
		l.Push(lua.LString("excel.Rows{}"))
	}
	return 1
}
