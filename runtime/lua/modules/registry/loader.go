package registry

import (
	"fmt"
	"sync"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	loaderModuleName        = "loader"
	loaderInstanceMetatable = "registry.Loader"
)

// LoaderModule represents the registry.loader submodule
type LoaderModule struct {
	log         *zap.Logger
	once        sync.Once
	moduleTable *lua.LTable
}

// LoaderInstance represents a filesystem-bound loader instance
type LoaderInstance struct {
	fs           fsapi.FS
	dtt          payload.Transcoder
	log          *zap.Logger
	interpolator *interpolate.Helper
	folderLoader *loader.Loader
}

// NewLoaderModule creates a new loader module
func NewLoaderModule(log *zap.Logger) *LoaderModule {
	if log == nil {
		log = zap.NewNop()
	}
	return &LoaderModule{
		log: log,
	}
}

// Info returns module metadata
func (m *LoaderModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        loaderModuleName,
		Description: "Registry loader for filesystem-bound loading",
		Class:       []string{luaapi.ClassStorage, luaapi.ClassIO},
	}
}

// Loader loads the module into the Lua state
func (m *LoaderModule) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 1)

		mt := l.NewTypeMetatable(loaderInstanceMetatable)
		methods := l.NewTable()
		l.SetFuncs(methods, map[string]lua.LGFunction{
			"load_directory": loaderLoadDirectory,
			"load_file":      loaderLoadFile,
		})
		mt.RawSetString("__index", methods)

		mod.RawSetString("new", l.NewFunction(m.createLoader))
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// createLoader creates a new loader instance bound to a filesystem
func (m *LoaderModule) createLoader(l *lua.LState) int {
	ctx := l.Context()

	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("transcoder not found in context"))
		return 2
	}

	fsRegistry := fsapi.GetRegistry(ctx)
	if fsRegistry == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem registry not found in context"))
		return 2
	}

	fsName := l.CheckString(1)
	if fsName == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem name required"))
		return 2
	}

	fsys, ok := fsRegistry.GetFS(fsName)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("filesystem '%s' not found", fsName)))
		return 2
	}

	interpolators := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)

	folderLoader := loader.NewLoader(dtt, m.log, interpolators)

	loaderInstance := &LoaderInstance{
		fs:           fsys,
		dtt:          dtt,
		log:          m.log,
		interpolator: interpolators,
		folderLoader: folderLoader,
	}

	ud := l.NewUserData()
	ud.Value = loaderInstance
	l.SetMetatable(ud, l.GetTypeMetatable(loaderInstanceMetatable))

	l.Push(ud)
	return 1
}

// loaderLoadDirectory loads entries from a directory
func loaderLoadDirectory(l *lua.LState) int {
	fl := checkLoaderInstance(l)
	if fl == nil {
		return 0
	}

	dirPath := l.CheckString(2)
	if dirPath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("directory path required"))
		return 2
	}

	entries, err := fl.folderLoader.LoadDir(l.Context(), fl.fs, dirPath)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to load entries: %v", err)))
		return 2
	}

	entriesTable := l.CreateTable(0, len(entries))
	for i, entry := range entries {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert entry: %v", err)))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
}

// loaderLoadFile loads entries from a single file
func loaderLoadFile(l *lua.LState) int {
	fl := checkLoaderInstance(l)
	if fl == nil {
		return 0
	}

	filePath := l.CheckString(2)
	if filePath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("file path required"))
		return 2
	}

	entries, err := fl.folderLoader.LoadFile(l.Context(), fl.fs, filePath)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to load entries: %v", err)))
		return 2
	}

	entriesTable := l.NewTable()
	for i, entry := range entries {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert entry: %v", err)))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
}

// checkLoaderInstance checks if the first argument is a LoaderInstance userdata
func checkLoaderInstance(l *lua.LState) *LoaderInstance {
	ud := l.CheckUserData(1)
	if fl, ok := ud.Value.(*LoaderInstance); ok {
		return fl
	}
	l.ArgError(1, "loader instance expected")
	return nil
}
