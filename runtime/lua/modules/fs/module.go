package fs

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	lua "github.com/yuin/gopher-lua"
	"io/fs"
)

// Module represents a fs Lua module
type Module struct{}

const (
	// Type constants
	typeFile = "file"
	typeDir  = "directory"

	// Seek constants
	seekSet = "set"
	seekCur = "cur"
	seekEnd = "end"
)

// NewFSModule creates and returns a new instance of the fs Module
func NewFSModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "fs"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	// Register type constants
	typeTable := l.CreateTable(0, 2)
	typeTable.RawSetString("FILE", lua.LString(typeFile))
	typeTable.RawSetString("DIR", lua.LString(typeDir))
	l.SetField(t, "type", typeTable)

	// Register seek constants
	seekTable := l.CreateTable(0, 3)
	seekTable.RawSetString("SET", lua.LString(seekSet))
	seekTable.RawSetString("CUR", lua.LString(seekCur))
	seekTable.RawSetString("END", lua.LString(seekEnd))
	l.SetField(t, "seek", seekTable)

	// Register core functions
	api := map[string]lua.LGFunction{
		"get": apiGet,
	}

	l.SetFuncs(t, api)

	registerFS(l, t)
	registerFile(l)

	l.Push(t)
	return 1
}

func apiGet(l *lua.LState) int {
	name := l.CheckString(1)
	if name == "" {
		l.ArgError(1, "filesystem name required")
		return 0
	}

	reg := fsapi.GetRegistry(l.Context())
	if reg == nil {
		l.RaiseError("no filesystem registry in context")
		return 0
	}

	f, ok := reg.GetFS(name)
	if !ok {
		l.RaiseError("filesystem not found: %s", name)
		return 0
	}

	l.Push(WrapFS(l, f))
	return 1
}

func CheckFS(l *lua.LState, n int) *FS {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*FS); ok {
		return v
	}

	l.ArgError(n, "filesystem expected")
	return nil
}

func WrapFS(l *lua.LState, fs fsapi.FS) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &FS{fs: fs}
	l.SetMetatable(ud, l.GetTypeMetatable("fs.FS"))
	return ud
}

func CheckFile(l *lua.LState, n int) *File {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*File); ok {
		return v
	}
	l.ArgError(n, "file expected")
	return nil
}

func WrapFile(l *lua.LState, file fsapi.File) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &File{file: file}
	l.SetMetatable(ud, l.GetTypeMetatable("fs.File"))
	return ud
}

func pushFileInfo(l *lua.LState, info fs.FileInfo) *lua.LTable {
	t := l.NewTable()
	t.RawSetString("name", lua.LString(info.Name()))
	t.RawSetString("size", lua.LNumber(info.Size()))
	t.RawSetString("mode", lua.LNumber(uint32(info.Mode())))
	t.RawSetString("modified", lua.LNumber(info.ModTime().Unix()))
	t.RawSetString("is_dir", lua.LBool(info.IsDir()))
	t.RawSetString("type", lua.LString(typeFile))
	if info.IsDir() {
		t.RawSetString("type", lua.LString(typeDir))
	}
	return t
}
