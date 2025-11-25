// Package excel provides a Lua module for working with Excel files
package excel

import (
	"io"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the excel Lua module
type Module struct {
	log         *zap.Logger
	once        sync.Once
	moduleTable *lua.LTable
}

// NewModule creates a new Excel module
func NewModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "excel",
		Description: "Excel file operations",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassIO},
	}
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 2)
		mod.RawSetString("new", l.NewFunction(excelNew))
		mod.RawSetString("open", l.NewFunction(excelOpen))
		registerWorkbook(l)
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// excelNew creates a new Excel workbook
func excelNew(l *lua.LState) int {
	workbook, err := NewWorkbook()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newExcelOperationError(l, err, "new"))
		return 2
	}

	// Create and return userdata
	ud := l.NewUserData()
	ud.Value = workbook
	l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// excelOpen opens an existing Excel file from a reader
func excelOpen(l *lua.LState) int {
	// Check if the argument is a reader
	ud := l.CheckUserData(1)

	// Attempt to get a reader from userdata
	reader, ok := ud.Value.(io.Reader)
	if !ok {
		l.ArgError(1, "expected reader object")
		return 0
	}

	workbook, err := OpenReader(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newExcelOperationError(l, err, "open"))
		return 2
	}

	// Create and return userdata
	result := l.NewUserData()
	result.Value = workbook
	l.SetMetatable(result, l.GetTypeMetatable("Workbook"))
	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
