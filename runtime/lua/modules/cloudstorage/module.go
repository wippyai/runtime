package cloudstorage

import (
	"fmt"

	csapi "github.com/ponyruntime/pony/api/cloudstorage"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Module represents a cloudstorage Lua module
type Module struct{}

// NewModule creates and returns a new instance of the cloudstorage Module
func NewModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "cloudstorage"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	t := l.CreateTable(0, 1) // 1 field: get function

	// Register the get function
	t.RawSetString("get", l.NewFunction(apiGet))

	// Register CloudStorage type
	registerCloudStorage(l)

	l.Push(t)
	return 1
}

func apiGet(l *lua.LState) int {
	// Get resource ID
	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource ID is required"))
		return 2
	}

	// Get Unit of Work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found in context"))
		return 2
	}
	// add release

	// Get resource registry
	reg := resource.GetResources(uw.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	// Parse resource ID
	resID := registry.ParseID(id)

	// Acquire resource
	res, err := reg.Acquire(uw.Context(), resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	// Get CloudStorage instance
	storageRes, err := res.Get()
	if err != nil {
		// Release resource immediately since we failed
		releaseErr := res.Release()
		if releaseErr != nil {
			// Log error (would use zap logger in production)
			fmt.Printf("failed to release resource after get failure: %v, %v\n", err, releaseErr)
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	// Check if it's a CloudStorageResource
	csRes, ok := storageRes.(csapi.Storage)
	if !ok {
		// Release resource immediately since it's not the right type
		releaseErr := res.Release()
		if releaseErr != nil {
			// Log error (would use zap logger in production)
			fmt.Printf("failed to release non-CloudStorage resource: %T, %v\n", storageRes, releaseErr)
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a cloud storage provider: %T", storageRes)))
		return 2
	}

	// Create userdata and wrap
	ud := WrapCloudStorage(l, csRes)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func CheckCloudStorage(l *lua.LState, n int) *CloudStorage {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*CloudStorage); ok {
		return v
	}

	l.ArgError(n, "cloudstorage expected")
	return nil
}

func WrapCloudStorage(l *lua.LState, storage csapi.Storage) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = NewCloudStorage(storage)
	l.SetMetatable(ud, l.GetTypeMetatable("cloudstorage.Storage"))
	return ud
}

// pushObjectMetadata creates a Lua table from a ObjectMetadata
func pushObjectMetadata(l *lua.LState, meta csapi.ObjectMetadata) *lua.LTable {
	t := l.NewTable()
	t.RawSetString("key", lua.LString(meta.Key))
	t.RawSetString("size", lua.LNumber(meta.Size))
	t.RawSetString("content_type", lua.LString(meta.ContentType))
	t.RawSetString("etag", lua.LString(meta.ETag))
	return t
}

// pushListObjectsResult creates a Lua table from a ListObjectsResult
func pushListObjectsResult(l *lua.LState, result *csapi.ListObjectsResult) *lua.LTable {
	t := l.NewTable()

	// Create objects table
	objects := l.NewTable()
	for i, obj := range result.Objects {
		objects.RawSetInt(i+1, pushObjectMetadata(l, obj))
	}
	t.RawSetString("objects", objects)

	t.RawSetString("is_truncated", lua.LBool(result.IsTruncated))
	t.RawSetString("next_continuation_token", lua.LString(result.NextContinuationToken))

	return t
}
