// Package excel provides Excel file operations for engine.
package excel

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/resource"
	lua2api "github.com/wippyai/runtime/api/runtime/lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/xuri/excelize/v2"
	lua "github.com/yuin/gopher-lua"
)

const workbookTypeName = "Workbook"

var (
	moduleTable       *lua.LTable
	registration      *lua2api.Registration
	workbookMetatable *lua.LTable
	initOnce          sync.Once
)

// Module is the singleton excel module instance.
var Module = &excelModule{}

type excelModule struct{}

func (m *excelModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "excel",
		Description: "Excel file operations",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *excelModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("new", lua.LGoFunc(excelNew))
		mod.RawSetString("open", lua.LGoFunc(excelOpen))
		mod.Immutable = true
		moduleTable = mod

		workbookMetatable = value.RegisterTypeMethods(nil, workbookTypeName,
			map[string]lua.LGFunction{"__tostring": workbookToString},
			workbookMethods)

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *excelModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Workbook wraps excelize.File with cleanup tracking.
type Workbook struct {
	file          *excelize.File
	closed        bool
	mu            sync.Mutex
	cancelCleanup func()
}

// NewWorkbook creates a new empty Excel workbook.
func NewWorkbook(ctx context.Context) *Workbook {
	wb := &Workbook{
		file:   excelize.NewFile(),
		closed: false,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		wb.cancelCleanup = store.AddCleanup(func() error {
			wb.mu.Lock()
			defer wb.mu.Unlock()
			if !wb.closed && wb.file != nil {
				wb.file.Close()
				wb.closed = true
			}
			return nil
		})
	}

	return wb
}

// WrapFile wraps an existing excelize.File with cleanup tracking.
func WrapFile(ctx context.Context, file *excelize.File) *Workbook {
	wb := &Workbook{
		file:   file,
		closed: false,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		wb.cancelCleanup = store.AddCleanup(func() error {
			wb.mu.Lock()
			defer wb.mu.Unlock()
			if !wb.closed && wb.file != nil {
				wb.file.Close()
				wb.closed = true
			}
			return nil
		})
	}

	return wb
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

var workbookMethods = map[string]lua.LGFunction{
	"new_sheet":      workbookNewSheet,
	"get_sheet_list": workbookGetSheetList,
	"get_rows":       workbookGetRows,
	"set_cell_value": workbookSetCellValue,
	"write_to":       workbookWriteTo,
	"close":          workbookClose,
}

func checkWorkbook(l *lua.LState, idx int) *Workbook {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Workbook); ok {
		return v
	}
	l.ArgError(idx, "Workbook expected")
	return nil
}

func excelNew(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	wb := NewWorkbook(ctx)

	value.NewUserData(l, wb, workbookMetatable)
	l.Push(lua.LNil)
	return 2
}

func excelOpen(l *lua.LState) int {
	ud := l.CheckUserData(1)
	stream, ok := ud.Value.(*stream.Stream)
	if !ok {
		l.ArgError(1, "Stream expected")
		return 0
	}

	yield := AcquireOpenStreamYield()
	yield.StreamID = stream.ID
	l.Push(yield)
	return -1
}

func workbookNewSheet(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook is closed"))
		return 2
	}
	wb.mu.Unlock()

	name := l.CheckString(2)
	index, err := wb.file.NewSheet(name)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("create sheet: %v", err)))
		return 2
	}

	l.Push(lua.LNumber(index))
	l.Push(lua.LNil)
	return 2
}

func workbookGetSheetList(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook is closed"))
		return 2
	}
	wb.mu.Unlock()

	sheets := wb.file.GetSheetList()
	sheetList := l.CreateTable(len(sheets), 0)
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
		return 0
	}

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook is closed"))
		return 2
	}
	wb.mu.Unlock()

	sheetName := l.CheckString(2)
	rows, err := wb.file.GetRows(sheetName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("get rows: %v", err)))
		return 2
	}

	luaRows := l.CreateTable(len(rows), 0)
	for rowIdx, row := range rows {
		luaRow := l.CreateTable(len(row), 0)
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
		return 0
	}

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		l.Push(lua.LString("workbook is closed"))
		return 1
	}
	wb.mu.Unlock()

	sheetName := l.CheckString(2)
	cellRef := l.CheckString(3)
	value := l.CheckAny(4)

	var goValue interface{}
	switch v := value.(type) {
	case lua.LString:
		goValue = string(v)
	case lua.LNumber:
		goValue = float64(v)
	case lua.LInteger:
		goValue = int64(v)
	case lua.LBool:
		goValue = bool(v)
	default:
		goValue = v.String()
	}

	err := wb.file.SetCellValue(sheetName, cellRef, goValue)
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("set cell value: %v", err)))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

func workbookWriteTo(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		l.Push(lua.LString("workbook is closed"))
		return 1
	}
	wb.mu.Unlock()

	ud := l.CheckUserData(2)
	stream, ok := ud.Value.(*stream.Stream)
	if !ok {
		l.ArgError(2, "Stream expected")
		return 0
	}

	yield := AcquireWriteStreamYield()
	yield.File = wb.file
	yield.StreamID = stream.ID
	l.Push(yield)
	return -1
}

func workbookClose(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	wb.mu.Lock()
	if !wb.closed && wb.file != nil {
		wb.file.Close()
		wb.closed = true
		cancel := wb.cancelCleanup
		wb.cancelCleanup = nil
		wb.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		wb.mu.Unlock()
	}

	l.Push(lua.LNil)
	return 1
}

func workbookToString(l *lua.LState) int {
	wb := checkWorkbook(l, 1)
	if wb == nil {
		return 0
	}

	wb.mu.Lock()
	closed := wb.closed
	wb.mu.Unlock()

	if closed {
		l.Push(lua.LString("excel.Workbook{closed}"))
	} else {
		l.Push(lua.LString("excel.Workbook{}"))
	}
	return 1
}
