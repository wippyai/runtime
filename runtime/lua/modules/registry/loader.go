package registry

import (
	"fmt"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
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
	log *zap.Logger
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

// Name returns the module name
func (m *LoaderModule) Name() string {
	return loaderModuleName
}

// Loader loads the module into the Lua state
func (m *LoaderModule) Loader(l *lua.LState) int {
	// Create module table
	mod := l.CreateTable(0, 1)

	// Register the loader instance metatable
	mt := l.NewTypeMetatable(loaderInstanceMetatable)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"load_directory": loaderLoadDirectory,
		"load_file":      loaderLoadFile,
	}))

	// Add the "new" function to the module table
	mod.RawSetString("new", l.NewFunction(m.createLoader))

	// Push the module table
	l.Push(mod)
	return 1
}

// createLoader creates a new loader instance bound to a filesystem
func (m *LoaderModule) createLoader(l *lua.LState) int {
	// Get context from Lua state
	ctx := l.Context()

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("transcoder not found in context"))
		return 2
	}

	// Get filesystem registry from context
	fsRegistry := fsapi.GetRegistry(ctx)
	if fsRegistry == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem registry not found in context"))
		return 2
	}

	// Get filesystem name
	fsName := l.CheckString(1)
	if fsName == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("filesystem name required"))
		return 2
	}

	// Get the filesystem from the registry
	fsys, ok := fsRegistry.GetFS(fsName)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("filesystem '%s' not found", fsName)))
		return 2
	}

	// Create interpolator with FS support
	interpolators := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)

	// Create folder loader with FS support
	folderLoader := loader.NewLoader(dtt, m.log, interpolators)

	// Create loader instance
	loaderInstance := &LoaderInstance{
		fs:           fsys,
		dtt:          dtt,
		log:          m.log,
		interpolator: interpolators,
		folderLoader: folderLoader,
	}

	// Create userdata
	ud := l.NewUserData()
	ud.Value = loaderInstance
	l.SetMetatable(ud, l.GetTypeMetatable(loaderInstanceMetatable))

	l.Push(ud)
	return 1
}

// loaderLoadDirectory loads entries from a directory
func loaderLoadDirectory(l *lua.LState) int {
	// Get loader instance
	fl := checkLoaderInstance(l)
	if fl == nil {
		return 0
	}

	// Get directory path
	dirPath := l.CheckString(2)
	if dirPath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("directory path required"))
		return 2
	}

	// Load entries
	entries, err := fl.folderLoader.LoadDir(l.Context(), fl.fs, dirPath)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to load entries: %v", err)))
		return 2
	}

	// Convert entries to Lua table
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
	// Get loader instance
	fl := checkLoaderInstance(l)
	if fl == nil {
		return 0
	}

	// Get file path
	filePath := l.CheckString(2)
	if filePath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("file path required"))
		return 2
	}

	// Load entries from file
	entries, err := fl.folderLoader.LoadFile(l.Context(), fl.fs, filePath)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to load entries: %v", err)))
		return 2
	}

	// Convert entries to Lua table
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

// Helper function to check if the first argument is a LoaderInstance and return it
func checkLoaderInstance(l *lua.LState) *LoaderInstance {
	ud := l.CheckUserData(1)
	if fl, ok := ud.Value.(*LoaderInstance); ok {
		return fl
	}
	l.ArgError(1, "loader instance expected")
	return nil
}
