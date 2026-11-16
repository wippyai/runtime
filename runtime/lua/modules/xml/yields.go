// SPDX-License-Identifier: MPL-2.0

package xml

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	xmlapi "github.com/wippyai/runtime/api/xml"
)

// yieldValue gives yields the Lua value identity the scheduler requires.
type yieldValue struct{}

func (yieldValue) String() string       { return "<xml_yield>" }
func (yieldValue) Type() lua.LValueType { return lua.LTUserData }

// fillYield reads ahead and completes reader:next().
type fillYield struct {
	yieldValue
	r *Reader
}

func (y *fillYield) CmdID() dispatcher.CommandID { return xmlapi.ReaderFill }
func (y *fillYield) ToCommand() dispatcher.Command {
	return xmlapi.FillCmd{Reader: y.r.handle}
}
func (y *fillYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	return nextResult(l, y.r, err)
}

// nodeYield finishes reader:node().
type nodeYield struct {
	yieldValue
	r *Reader
}

func (y *nodeYield) CmdID() dispatcher.CommandID { return xmlapi.ReaderNode }
func (y *nodeYield) ToCommand() dispatcher.Command {
	return xmlapi.NodeCmd{Reader: y.r.handle}
}
func (y *nodeYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	return nodeResult(l, y.r, err)
}

// skipYield finishes reader:skip().
type skipYield struct {
	yieldValue
	r *Reader
}

func (y *skipYield) CmdID() dispatcher.CommandID { return xmlapi.ReaderSkip }
func (y *skipYield) ToCommand() dispatcher.Command {
	return xmlapi.SkipCmd{Reader: y.r.handle}
}
func (y *skipYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	return boolResult(l, err)
}

// copyYield finishes reader:copy().
type copyYield struct {
	yieldValue
	r *Reader
	w *Writer
}

func (y *copyYield) CmdID() dispatcher.CommandID { return xmlapi.ReaderCopy }
func (y *copyYield) ToCommand() dispatcher.Command {
	return xmlapi.CopyCmd{Reader: y.r.handle, Writer: y.w.handle}
}
func (y *copyYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	return boolResult(l, err)
}

// recordsYield reads the next batch of records.
type recordsYield struct {
	yieldValue
	r *Reader
	n int
}

func (y *recordsYield) CmdID() dispatcher.CommandID { return xmlapi.RecordsRead }
func (y *recordsYield) ToCommand() dispatcher.Command {
	return xmlapi.RecordsReadCmd{Reader: y.r.handle, Count: y.n}
}
func (y *recordsYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	return recordsResult(l, y.r, y.n, err)
}

// flushYield writes buffered output for a writer method, flush(), finish() or a buffered
// copy; a failed flush after a copy also makes the copying reader terminal.
type flushYield struct {
	yieldValue
	w      *Writer
	copier *Reader
	sync   bool
	finish bool
}

func (y *flushYield) CmdID() dispatcher.CommandID { return xmlapi.WriterFlush }
func (y *flushYield) ToCommand() dispatcher.Command {
	return xmlapi.FlushCmd{Writer: y.w.handle, Sync: y.sync}
}
func (y *flushYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil && y.copier != nil {
		err = y.copier.entry.Abort(err)
	}
	if y.finish {
		if err != nil {
			return []lua.LValue{lua.LNil, luaError(l, err)}
		}
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	return boolResult(l, err)
}
