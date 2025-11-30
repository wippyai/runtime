package excel

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

var getResources = engine.GetResources

// OpenStreamYield wraps ExcelOpenStreamCmd for Lua.
type OpenStreamYield struct {
	*excelapi.ExcelOpenStreamCmd
}

var openStreamYieldPool = sync.Pool{New: func() any { return &OpenStreamYield{} }}

func AcquireOpenStreamYield() *OpenStreamYield {
	y := openStreamYieldPool.Get().(*OpenStreamYield)
	y.ExcelOpenStreamCmd = excelapi.AcquireExcelOpenStreamCmd()
	return y
}

func ReleaseOpenStreamYield(y *OpenStreamYield) {
	if y.ExcelOpenStreamCmd != nil {
		y.ExcelOpenStreamCmd.Release()
		y.ExcelOpenStreamCmd = nil
	}
	openStreamYieldPool.Put(y)
}

func (y *OpenStreamYield) String() string                { return "<excel_open_stream_yield>" }
func (y *OpenStreamYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *OpenStreamYield) CmdID() dispatcher.CommandID   { return excelapi.CmdExcelOpenStream }
func (y *OpenStreamYield) ToCommand() dispatcher.Command { return y.ExcelOpenStreamCmd }
func (y *OpenStreamYield) Release()                      { ReleaseOpenStreamYield(y) }

// WriteStreamYield wraps ExcelWriteStreamCmd for Lua.
type WriteStreamYield struct {
	*excelapi.ExcelWriteStreamCmd
}

var writeStreamYieldPool = sync.Pool{New: func() any { return &WriteStreamYield{} }}

func AcquireWriteStreamYield() *WriteStreamYield {
	y := writeStreamYieldPool.Get().(*WriteStreamYield)
	y.ExcelWriteStreamCmd = excelapi.AcquireExcelWriteStreamCmd()
	return y
}

func ReleaseWriteStreamYield(y *WriteStreamYield) {
	if y.ExcelWriteStreamCmd != nil {
		y.ExcelWriteStreamCmd.Release()
		y.ExcelWriteStreamCmd = nil
	}
	writeStreamYieldPool.Put(y)
}

func (y *WriteStreamYield) String() string                { return "<excel_write_stream_yield>" }
func (y *WriteStreamYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *WriteStreamYield) CmdID() dispatcher.CommandID   { return excelapi.CmdExcelWriteStream }
func (y *WriteStreamYield) ToCommand() dispatcher.Command { return y.ExcelWriteStreamCmd }
func (y *WriteStreamYield) Release()                      { ReleaseWriteStreamYield(y) }
