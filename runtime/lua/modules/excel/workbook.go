package excel

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
	lua "github.com/yuin/gopher-lua"
)

// Workbook represents an Excel workbook.
type Workbook struct {
	File *excelize.File
}

// NewWorkbook creates a new Excel workbook.
func NewWorkbook() (*Workbook, error) {
	file := excelize.NewFile()
	return &Workbook{File: file}, nil
}

// OpenReader opens an Excel file from a reader,
func OpenReader(reader io.Reader) (*Workbook, error) {
	file, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("open Excel file from reader: %w", err)
	}
	return &Workbook{File: file}, nil
}

// Close closes the workbook and releases resources.
func (w *Workbook) Close() error {
	return w.File.Close()
}

// checkWorkbook checks if the userdata is a valid Workbook
func checkWorkbook(l *lua.LState, idx int) *Workbook {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Workbook); ok {
		return v
	}
	return nil
}

// registerWorkbook registers the Workbook type and its methods
func registerWorkbook(l *lua.LState) {
	mt := l.NewTypeMetatable("Workbook")
	indexTable := l.CreateTable(0, 5)
	l.SetField(indexTable, "new_sheet", l.NewFunction(workbookNewSheet))
	l.SetField(indexTable, "get_sheet_list", l.NewFunction(workbookGetSheetList))
	l.SetField(indexTable, "get_rows", l.NewFunction(workbookGetRows))
	l.SetField(indexTable, "set_cell_value", l.NewFunction(workbookSetCellValue))
	l.SetField(indexTable, "close", l.NewFunction(workbookClose))
	l.SetField(mt, "__index", indexTable)
}

// workbookNewSheet implements the new_sheet method
func workbookNewSheet(l *lua.LState) int {
	workbook := checkWorkbook(l, 1)
	if workbook == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook expected"))
		return 2
	}

	// Create the new sheet (or get existing index if sheet already exists)
	name := l.CheckString(2)
	index, err := workbook.File.NewSheet(name)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("create sheet: %v", err)))
		return 2
	}

	// Return the index of the sheet (1-based for Lua)
	l.Push(lua.LNumber(index))
	l.Push(lua.LNil)
	return 2
}

// workbookGetSheetList implements the get_sheet_list method
func workbookGetSheetList(l *lua.LState) int {
	workbook := checkWorkbook(l, 1)
	if workbook == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook expected"))
		return 2
	}

	// Get sheet list and create a table with appropriate capacity
	sheets := workbook.File.GetSheetList()
	sheetList := l.CreateTable(len(sheets), 0)

	for i, sheet := range sheets {
		sheetList.RawSetInt(i+1, lua.LString(sheet))
	}

	l.Push(sheetList)
	l.Push(lua.LNil)
	return 2
}

// workbookGetRows implements the get_rows method
func workbookGetRows(l *lua.LState) int {
	workbook := checkWorkbook(l, 1)
	if workbook == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("workbook expected"))
		return 2
	}

	sheetName := l.CheckString(2)

	// Get all rows from the sheet
	rows, err := workbook.File.GetRows(sheetName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get rows: %v", err)))
		return 2
	}

	// Create a Lua table with the appropriate capacity for rows
	luaRows := l.CreateTable(len(rows), 0)

	for rowIdx, row := range rows {
		// Create a table for each row with capacity for the columns
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

// workbookSetCellValue implements the set_cell_value method
func workbookSetCellValue(l *lua.LState) int {
	workbook := checkWorkbook(l, 1)
	if workbook == nil {
		l.Push(lua.LString("workbook expected"))
		return 1
	}

	sheetName := l.CheckString(2)
	cellRef := l.CheckString(3)
	value := l.CheckAny(4)

	// Use the generic SetCellValue method
	err := workbook.File.SetCellValue(sheetName, cellRef, value)
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("set cell value: %v", err)))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

// workbookClose implements the close method
func workbookClose(l *lua.LState) int {
	workbook := checkWorkbook(l, 1)
	if workbook == nil {
		l.Push(lua.LString("workbook expected"))
		return 1
	}

	err := workbook.Close()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("close workbook: %v", err)))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}
