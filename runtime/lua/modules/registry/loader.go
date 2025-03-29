package registry

import (
	"fmt"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	loaderModuleName = "loader"
)

// LoaderModule represents the registry.loader submodule
type LoaderModule struct {
	log *zap.Logger
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
	mod := l.CreateTable(0, 3)

	// Register functions
	mod.RawSetString("load_directory", l.NewFunction(m.loadDirectory))
	mod.RawSetString("load_file", l.NewFunction(m.loadFile))
	mod.RawSetString("load_directory_from_fs", l.NewFunction(m.loadDirectoryFromFS))

	// Push the module
	l.Push(mod)
	return 1
}

// loadDirectory loads entries from a directory
func (m *LoaderModule) loadDirectory(l *lua.LState) int {
	// Get context from Lua state
	ctx := l.Context()

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("transcoder not found in context"))
		return 2
	}

	// Get directory path
	dirPath := l.CheckString(1)
	if dirPath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("directory path required"))
		return 2
	}

	// Get variables table (optional)
	varsTable := l.OptTable(2, l.NewTable())

	// Convert Lua variables table to Go map
	vars := make(interpolate.Variables)
	varsTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok && v.Type() == lua.LTString {
			vars[string(kStr)] = v.String()
		}
	})

	// Create folder loader
	interpolatorHelper := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadVars),
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	folderLoader := loader.NewLoader(dtt, m.log, interpolatorHelper)

	// Load entries
	entries, err := folderLoader.LoadFolder(dirPath, vars)
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

// loadDirectoryFromFS loads entries from a directory in a specific filesystem
func (m *LoaderModule) loadDirectoryFromFS(l *lua.LState) int {
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

	// Get directory path
	dirPath := l.CheckString(2)
	if dirPath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("directory path required"))
		return 2
	}

	// Get variables table (optional)
	varsTable := l.OptTable(3, l.NewTable())

	// Convert Lua variables table to Go map
	vars := make(interpolate.Variables)
	varsTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok && v.Type() == lua.LTString {
			vars[string(kStr)] = v.String()
		}
	})

	// Get the filesystem from the registry
	fsys, ok := fsRegistry.GetFS(fsName)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("filesystem '%s' not found", fsName)))
		return 2
	}

	// Create interpolator with FS support
	interpolatorHelper := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadVars),
		interpolate.WithInterpolator(interpolate.FSAwareFileLoader(fsys)),
	)

	// Create folder loader with FS support
	folderLoader := loader.NewLoader(
		dtt,
		m.log,
		interpolatorHelper,
		loader.WithLoaderFS(fsys),
	)

	// Load entries
	entries, err := folderLoader.LoadFolder(dirPath, vars)
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

// loadFile loads entries from a single file
func (m *LoaderModule) loadFile(l *lua.LState) int {
	// Get context from Lua state
	ctx := l.Context()

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("transcoder not found in context"))
		return 2
	}

	// Get file path
	filePath := l.CheckString(1)
	if filePath == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("file path required"))
		return 2
	}

	// Get variables table (optional)
	varsTable := l.OptTable(2, l.NewTable())

	// Convert Lua variables table to Go map
	vars := make(interpolate.Variables)
	varsTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok && v.Type() == lua.LTString {
			vars[string(kStr)] = v.String()
		}
	})

	// Create interpolator
	interpolatorHelper := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadVars),
		interpolate.WithInterpolator(interpolate.LoadFile),
	)

	// Create loader
	entryLoader := loader.NewLoader(dtt, m.log, interpolatorHelper)

	// Load entries from file
	entries, err := entryLoader.LoadFile(filePath, vars)
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
