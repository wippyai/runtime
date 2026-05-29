// SPDX-License-Identifier: MPL-2.0

//go:build excel

// Package excel provides Excel file operations for Lua.
package excel

import (
	"context"
	"fmt"
	"io"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/xuri/excelize/v2"
)

const workbookTypeName = "excel.Workbook"

var (
	workbookMetatable *lua.LTable
	moduleTable       *lua.LTable
)

func init() {
	workbookMetatable = value.RegisterTypeMethods(nil, workbookTypeName,
		map[string]lua.LGoFunc{"__tostring": workbookToString},
		workbookMethods)

	moduleTable = lua.CreateTable(0, 2)
	moduleTable.RawSetString("new", lua.LGoFunc(excelNew))
	moduleTable.RawSetString("open", lua.LGoFunc(excelOpen))
	moduleTable.Immutable = true
}

// Module is the excel module definition.
var Module = &luaapi.ModuleDef{
	Name:        "excel",
	Description: "Excel file operations",
	Class:       []string{luaapi.ClassIO, luaapi.ClassEncoding},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, nil
	},
	Types: ModuleTypes,
}

// Workbook wraps excelize.File with cleanup tracking.
type Workbook struct {
	file          *excelize.File
	cancelCleanup func()
	closed        bool
}

// newWorkbook creates a workbook wrapper and registers cleanup with context.
func newWorkbook(ctx context.Context, file *excelize.File) *Workbook {
	wb := &Workbook{
		file:   file,
		closed: false,
	}

	if store := resource.GetStore(ctx); store != nil {
		wb.cancelCleanup = store.AddCleanup(func() error {
			if !wb.closed && wb.file != nil {
				_ = wb.file.Close()
				wb.closed = true
			}
			return nil
		})
	}

	return wb
}

// Close closes the workbook and cancels cleanup registration.
func (wb *Workbook) Close() {
	if wb.closed {
		return
	}

	if wb.file != nil {
		_ = wb.file.Close()
	}
	wb.closed = true

	if wb.cancelCleanup != nil {
		wb.cancelCleanup()
		wb.cancelCleanup = nil
	}
}

var workbookMethods = map[string]lua.LGoFunc{
	"new_sheet":      workbookNewSheet,
	"get_sheet_list": workbookGetSheetList,
	"get_rows":       workbookGetRows,
	"set_cell_value": workbookSetCellValue,
	"write_to":       workbookWriteTo,
	"close":          workbookClose,
}

func checkWorkbook(l *lua.LState, _ int) *Workbook {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Workbook); ok {
		return v
	}
	return nil
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalErrorMsg(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func singleError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(err)
	return 1
}

func singleErrorMsg(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(err)
	return 1
}

func excelNew(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return internalErrorMsg(l, "no context")
	}

	wb := newWorkbook(ctx, excelize.NewFile())

	value.PushUserData(l, wb, workbookMetatable)
	l.Push(lua.LNil)
	return 2
}

func excelOpen(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return internalErrorMsg(l, "no context")
	}

	ud := l.CheckUserData(1)
	reader, ok := ud.Value.(io.Reader)
	if !ok {
		l.ArgError(1, "expected reader object")
		return 0
	}

	file, err := excelize.OpenReader(reader)
	if err != nil {
		return internalError(l, fmt.Errorf("open Excel file: %w", err), "open")
	}

	wb := newWorkbook(ctx, file)

	value.PushUserData(l, wb, workbookMetatable)
	l.Push(lua.LNil)
	return 2
}

func workbookNewSheet(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return invalidError(l, "workbook expected")
	}

	if wb.closed {
		return internalErrorMsg(l, "workbook is closed")
	}

	name := l.CheckString(2)
	index, err := wb.file.NewSheet(name)
	if err != nil {
		return internalError(l, err, "create sheet")
	}

	l.Push(lua.LNumber(index))
	l.Push(lua.LNil)
	return 2
}

func workbookGetSheetList(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return invalidError(l, "workbook expected")
	}

	if wb.closed {
		return internalErrorMsg(l, "workbook is closed")
	}

	sheets := wb.file.GetSheetList()
	sheetList := lua.CreateTable(len(sheets), 0)
	for i, sheet := range sheets {
		sheetList.RawSetInt(i+1, lua.LString(sheet))
	}

	l.Push(sheetList)
	l.Push(lua.LNil)
	return 2
}

func workbookGetRows(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return invalidError(l, "workbook expected")
	}

	if wb.closed {
		return internalErrorMsg(l, "workbook is closed")
	}

	sheetName := l.CheckString(2)
	rows, err := wb.file.GetRows(sheetName)
	if err != nil {
		return internalError(l, err, "get rows")
	}

	luaRows := lua.CreateTable(len(rows), 0)
	for rowIdx, row := range rows {
		luaRow := lua.CreateTable(len(row), 0)
		for colIdx, cellValue := range row {
			luaRow.RawSetInt(colIdx+1, lua.LString(cellValue))
		}
		luaRows.RawSetInt(rowIdx+1, luaRow)
	}

	l.Push(luaRows)
	l.Push(lua.LNil)
	return 2
}

func workbookSetCellValue(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return singleErrorMsg(l, "workbook expected")
	}

	if wb.closed {
		return singleErrorMsg(l, "workbook is closed")
	}

	sheetName := l.CheckString(2)
	cellRef := l.CheckString(3)
	val := l.CheckAny(4)

	err := wb.file.SetCellValue(sheetName, cellRef, value.ToGoAny(val))
	if err != nil {
		return singleError(l, err, "set cell value")
	}

	l.Push(lua.LNil)
	return 1
}

func workbookWriteTo(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return singleErrorMsg(l, "workbook expected")
	}

	if wb.closed {
		return singleErrorMsg(l, "workbook is closed")
	}

	ud := l.CheckUserData(2)
	if ud == nil {
		return singleErrorMsg(l, "writer expected")
	}

	writer, ok := ud.Value.(io.Writer)
	if !ok {
		return singleErrorMsg(l, "value does not implement io.Writer")
	}

	err := wb.file.Write(writer)
	if err != nil {
		return singleError(l, err, "write workbook")
	}

	l.Push(lua.LNil)
	return 1
}

func workbookClose(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return singleErrorMsg(l, "workbook expected")
	}

	wb.Close()

	l.Push(lua.LNil)
	return 1
}

func workbookToString(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	if wb.closed {
		l.Push(lua.LString("excel.Workbook{closed}"))
	} else {
		l.Push(lua.LString("excel.Workbook{}"))
	}
	return 1
}
