package fs

import (
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const (
	fsTypeName   = "fs.FS"
	fileTypeName = "fs.File"

	typeFile = "file"
	typeDir  = "directory"

	seekSet = "set"
	seekCur = "cur"
	seekEnd = "end"
)

var (
	fsMetatable          *lua.LTable
	fileMetatable        *lua.LTable
	fileScannerMetatable *lua.LTable
)

func init() {
	fsMetatable = value.RegisterTypeMethods(nil, fsTypeName,
		map[string]lua.LGoFunc{"__tostring": fsToString},
		fsMethods)
	fileMetatable = value.RegisterTypeMethods(nil, fileTypeName,
		map[string]lua.LGoFunc{"__tostring": fileToString},
		fileMethods)
	fileScannerMetatable = value.RegisterTypeMethods(nil, fileScannerTypeName,
		nil,
		fileScannerMethods)
}

// Module is the fs module definition.
var Module = &luaapi.ModuleDef{
	Name:        "fs",
	Description: "Filesystem operations",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 4)

	typeTable := lua.CreateTable(0, 2)
	typeTable.RawSetString("FILE", lua.LString(typeFile))
	typeTable.RawSetString("DIR", lua.LString(typeDir))
	typeTable.Immutable = true
	mod.RawSetString("type", typeTable)

	seekTable := lua.CreateTable(0, 3)
	seekTable.RawSetString("SET", lua.LString(seekSet))
	seekTable.RawSetString("CUR", lua.LString(seekCur))
	seekTable.RawSetString("END", lua.LString(seekEnd))
	seekTable.Immutable = true
	mod.RawSetString("seek", seekTable)

	mod.RawSetString("get", lua.LGoFunc(fsGet))

	mod.Immutable = true
	return mod, nil
}

func fsGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	name := l.CheckString(1)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem name required"))
		return 2
	}

	if !security.IsAllowed(ctx, "fs.get", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to access filesystem: " + name))
		return 2
	}

	reg := fsapi.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no filesystem registry in context"))
		return 2
	}

	f, ok := reg.GetFS(name)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem not found: " + name))
		return 2
	}

	value.PushUserData(l, NewFS(f, "."), fsMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkFS(l *lua.LState, idx int) *FS { //nolint:unparam
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*FS); ok {
		return v
	}
	l.ArgError(idx, "filesystem expected")
	return nil
}

func checkFile(l *lua.LState, idx int) *File { //nolint:unparam
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*File); ok {
		return v
	}
	l.ArgError(idx, "file expected")
	return nil
}

func pushFileInfo(l *lua.LState, info fs.FileInfo) *lua.LTable {
	t := l.NewTable()
	t.RawSetString("name", lua.LString(info.Name()))
	t.RawSetString("size", lua.LNumber(info.Size()))
	t.RawSetString("mode", lua.LNumber(info.Mode()))
	t.RawSetString("modified", lua.LNumber(info.ModTime().Unix()))
	t.RawSetString("is_dir", lua.LBool(info.IsDir()))
	t.RawSetString("type", lua.LString(typeFile))
	if info.IsDir() {
		t.RawSetString("type", lua.LString(typeDir))
	}
	return t
}
