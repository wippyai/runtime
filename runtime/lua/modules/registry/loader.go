package registry

import (
	lua "github.com/wippyai/go-lua"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"go.uber.org/zap"
)

const (
	loaderModuleName        = "loader"
	loaderInstanceMetatable = "registry.Loader"
)

func init() {
	value.RegisterTypeMethods(nil, loaderInstanceMetatable, nil,
		map[string]lua.LGoFunc{
			"load_directory": loaderLoadDirectory,
			"load_file":      loaderLoadFile,
		})
}

// LoaderInstance represents a filesystem-bound loader instance
type LoaderInstance struct {
	fs           fsapi.FS
	dtt          payload.Transcoder
	log          *zap.Logger
	interpolator *interpolate.Helper
	folderLoader *loader.Loader
}

// LoaderOptions for loader module configuration.
type LoaderOptions struct {
	Log *zap.Logger
}

// DefaultLoaderOptions returns default configuration.
func DefaultLoaderOptions() LoaderOptions {
	return LoaderOptions{
		Log: zap.NewNop(),
	}
}

// LoaderModule is the default loader module with default options.
var LoaderModule = NewLoaderModule(DefaultLoaderOptions())

// NewLoaderModule creates a loader module with given options.
func NewLoaderModule(opts LoaderOptions) *luaapi.ModuleDef {
	if opts.Log == nil {
		opts.Log = zap.NewNop()
	}

	return &luaapi.ModuleDef{
		Name:        loaderModuleName,
		Description: "Registry loader for filesystem-bound loading",
		Class:       []string{luaapi.ClassStorage, luaapi.ClassIO},
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			mod := lua.CreateTable(0, 1)
			mod.RawSetString("new", makeCreateLoader(opts.Log))
			mod.Immutable = true
			return mod, nil
		},
	}
}

// makeCreateLoader creates a loader factory function with the given logger
func makeCreateLoader(log *zap.Logger) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()

		dtt := payload.GetTranscoder(ctx)
		if dtt == nil {
			err := lua.NewLuaError(l, "transcoder not found in context").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		fsRegistry := fsapi.GetRegistry(ctx)
		if fsRegistry == nil {
			err := lua.NewLuaError(l, "filesystem registry not found in context").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		fsName := l.CheckString(1)
		if fsName == "" {
			err := lua.NewLuaError(l, "filesystem name required").
				WithKind(lua.Invalid).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		fsys, ok := fsRegistry.GetFS(fsName)
		if !ok {
			err := lua.NewLuaError(l, "filesystem '"+fsName+"' not found").
				WithKind(lua.NotFound).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		interpolators := interpolate.NewEntryInterpolator(dtt,
			interpolate.WithInterpolator(interpolate.LoadFile),
		)

		folderLoader := loader.NewLoader(dtt, log, interpolators)

		loaderInstance := &LoaderInstance{
			fs:           fsys,
			dtt:          dtt,
			log:          log,
			interpolator: interpolators,
			folderLoader: folderLoader,
		}

		value.PushTypedUserData(l, loaderInstance, loaderInstanceMetatable)
		l.Push(lua.LNil)
		return 2
	}
}

// loaderLoadDirectory loads entries from a directory
func loaderLoadDirectory(l *lua.LState) int {
	fl := checkLoaderInstance(l)
	if fl == nil {
		return 0
	}

	dirPath := l.CheckString(2)
	if dirPath == "" {
		err := lua.NewLuaError(l, "directory path required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entries, loadErr := fl.folderLoader.LoadDir(l.Context(), fl.fs, dirPath)
	if loadErr != nil {
		err := lua.WrapErrorWithLua(l, loadErr, "load entries").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	for i, entry := range entries {
		entryTable, convErr := entryToLuaTable(l, entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
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
		err := lua.NewLuaError(l, "file path required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entries, loadErr := fl.folderLoader.LoadFile(l.Context(), fl.fs, filePath)
	if loadErr != nil {
		err := lua.WrapErrorWithLua(l, loadErr, "load entries").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	for i, entry := range entries {
		entryTable, convErr := entryToLuaTable(l, entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
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
