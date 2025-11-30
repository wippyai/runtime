// Package excelapi provides excel command types for the dispatcher system.
package excelapi

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/xuri/excelize/v2"
)

func init() {
	dispatcher.MustRegisterCommands("excel",
		CmdExcelOpenStream, CmdExcelWriteStream,
	)
}

// Command IDs for excel operations.
// Range 130-139 is reserved for excel commands.
const (
	CmdExcelOpenStream  dispatcher.CommandID = 130 // Open Excel from stream
	CmdExcelWriteStream dispatcher.CommandID = 131 // Write Excel to stream
)

// ExcelOpenStreamCmd opens an Excel file from a registered stream.
type ExcelOpenStreamCmd struct {
	StreamID uint64
}

var openStreamCmdPool = sync.Pool{New: func() any { return &ExcelOpenStreamCmd{} }}

func AcquireExcelOpenStreamCmd() *ExcelOpenStreamCmd {
	return openStreamCmdPool.Get().(*ExcelOpenStreamCmd)
}
func (c *ExcelOpenStreamCmd) CmdID() dispatcher.CommandID { return CmdExcelOpenStream }
func (c *ExcelOpenStreamCmd) Release() {
	c.StreamID = 0
	openStreamCmdPool.Put(c)
}

// ExcelOpenStreamResponse contains the result of opening an Excel file.
type ExcelOpenStreamResponse struct {
	File  *excelize.File
	Error error
}

// ExcelWriteStreamCmd writes an Excel file to a registered stream.
type ExcelWriteStreamCmd struct {
	File     *excelize.File
	StreamID uint64
}

var writeStreamCmdPool = sync.Pool{New: func() any { return &ExcelWriteStreamCmd{} }}

func AcquireExcelWriteStreamCmd() *ExcelWriteStreamCmd {
	return writeStreamCmdPool.Get().(*ExcelWriteStreamCmd)
}
func (c *ExcelWriteStreamCmd) CmdID() dispatcher.CommandID { return CmdExcelWriteStream }
func (c *ExcelWriteStreamCmd) Release() {
	c.File = nil
	c.StreamID = 0
	writeStreamCmdPool.Put(c)
}

// ExcelWriteStreamResponse contains the result of writing an Excel file.
type ExcelWriteStreamResponse struct {
	Error error
}
