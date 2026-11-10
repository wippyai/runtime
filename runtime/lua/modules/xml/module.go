// SPDX-License-Identifier: MPL-2.0

// Package xml provides the xml Lua module: namespace-aware XML reading and writing.
package xml

import (
	"strings"
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/xml"
)

// Module is the xml module definition. String sources and in-memory writers never
// yield; stream operations need a stream handle, which deterministic contexts cannot obtain.
var Module = &luaapi.ModuleDef{
	Name:        "xml",
	Description: "Namespace-aware XML reading and writing with bounded memory",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

var registerOnce sync.Once

func registerTypes() {
	registerOnce.Do(func() {
		readerMetatable = value.RegisterTypeMethods(nil, readerTypeName, nil, readerMethods)
		recordsMetatable = value.RegisterTypeMethods(nil, recordsTypeName, nil, recordsMethods)
		writerMetatable = value.RegisterTypeMethods(nil, writerTypeName, nil, writerMethods)
	})
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	registerTypes()
	mod := lua.CreateTable(0, 10)
	mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
	mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
	mod.RawSetString("text", lua.LGoFunc(textFunc))
	mod.RawSetString("attr", lua.LGoFunc(attrFunc))
	mod.RawSetString("child", lua.LGoFunc(childFunc))
	mod.RawSetString("children", lua.LGoFunc(childrenFunc))
	mod.RawSetString("find", lua.LGoFunc(findFunc))
	mod.RawSetString("resolve", lua.LGoFunc(resolveFunc))
	mod.RawSetString("reader", lua.LGoFunc(readerFunc))
	mod.RawSetString("writer", lua.LGoFunc(writerFunc))
	mod.Immutable = true
	return mod, []luaapi.YieldType{
		{Sample: &fillYield{}, CmdID: xmlapi.ReaderFill},
		{Sample: &nodeYield{}, CmdID: xmlapi.ReaderNode},
		{Sample: &skipYield{}, CmdID: xmlapi.ReaderSkip},
		{Sample: &copyYield{}, CmdID: xmlapi.ReaderCopy},
		{Sample: &recordsYield{}, CmdID: xmlapi.RecordsRead},
		{Sample: &flushYield{}, CmdID: xmlapi.WriterFlush},
	}
}

func decodeFunc(l *lua.LState) int {
	data, ok := l.Get(1).(lua.LString)
	if !ok {
		return fail(l, lua.LNil, argument(l, "data must be a string"))
	}
	opts, lerr := readOptions(l, 2)
	if lerr != nil {
		return fail(l, lua.LNil, lerr)
	}
	d, err := xml.NewDecoder(opContext(l), strings.NewReader(string(data)), opts)
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	defer d.Close()
	b := newBuilder(newInterner(), 1)
	if err = d.VisitDocument(b.event); err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	roots := b.roots
	l.Push(roots.RawGetInt(1))
	l.Push(lua.LNil)
	return 2
}

func encodeFunc(l *lua.LState) int {
	element, ok := l.Get(1).(*lua.LTable)
	if !ok {
		return fail(l, lua.LNil, argument(l, "element must be a table"))
	}
	opts, lerr := writeOptions(l, 2)
	if lerr != nil {
		return fail(l, lua.LNil, lerr)
	}
	w, err := xml.NewEncoder(opts)
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	w.BeginTree()
	if err = writeTree(w, element); err == nil {
		err = w.Finish()
	}
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	l.Push(lua.LString(w.Drain()))
	l.Push(lua.LNil)
	return 2
}
