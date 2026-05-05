// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"io"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

const storageTypeName = "cloudstorage.Storage"

var Module = &luaapi.ModuleDef{
	Name:        "cloudstorage",
	Description: "Cloud storage operations (S3, GCS, etc.)",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassNetwork, luaapi.ClassIO},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := &lua.LTable{}
		mod.RawSetString("get", lua.LGoFunc(apiGet))
		mod.Immutable = true

		value.RegisterTypeMethods(nil, storageTypeName, storageMetamethods, storageMethods)

		return mod, []luaapi.YieldType{
			{Sample: &ListObjectsYield{}, CmdID: csapi.ListObjects},
			{Sample: &DownloadObjectYield{}, CmdID: csapi.DownloadObject},
			{Sample: &UploadObjectYield{}, CmdID: csapi.UploadObject},
			{Sample: &DeleteObjectsYield{}, CmdID: csapi.DeleteObjects},
			{Sample: &PresignedGetURLYield{}, CmdID: csapi.PresignedGetURL},
			{Sample: &PresignedPutURLYield{}, CmdID: csapi.PresignedPutURL},
			{Sample: &HeadObjectYield{}, CmdID: csapi.HeadObject},
		}
	},
	Types: ModuleTypes,
}

// storageWrapper wraps a cloud storage instance with resource tracking.
type storageWrapper struct {
	storage   csapi.Storage
	resource  resource.Resource[any]
	onRelease context.CancelFunc
	released  bool
}

var storageMethods = map[string]lua.LGoFunc{
	"list_objects":      storageListObjects,
	"head_object":       storageHeadObject,
	"download_object":   storageDownloadObject,
	"upload_object":     storageUploadObject,
	"delete_objects":    storageDeleteObjects,
	"presigned_get_url": storagePresignedGetURL,
	"presigned_put_url": storagePresignedPutURL,
	"release":           storageRelease,
}

var storageMetamethods = map[string]lua.LGoFunc{
	"__tostring": storageToString,
}

func checkStorage(l *lua.LState, _ int) *storageWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*storageWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "cloudstorage.Storage expected")
	return nil
}

func apiGet(l *lua.LState) int {
	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource ID is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	ctx := l.Context()
	store := rtresource.GetStore(ctx)
	if store == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource store not found").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource registry not found").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	resID := registry.ParseID(id)

	if !security.IsAllowed(ctx, "cloudstorage.get", resID.String(), nil) {
		l.RaiseError("not allowed to access cloud storage resource: %s", resID.String())
		return 0
	}

	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.NotFound).WithRetryable(false))
		return 2
	}

	onRelease := store.AddCleanup(func() error {
		res.Release()
		return nil
	})

	storageRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	csRes, ok := storageRes.(csapi.Storage)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource is not a cloud storage provider").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	wrapper := &storageWrapper{
		storage:   csRes,
		resource:  res,
		onRelease: onRelease,
	}

	value.PushTypedUserData(l, wrapper, storageTypeName)
	l.Push(lua.LNil)
	return 2
}

func storageToString(l *lua.LState) int {
	l.Push(lua.LString("cloudstorage.Storage"))
	return 1
}

func storageRelease(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LTrue)
		return 1
	}

	if wrapper.resource != nil {
		wrapper.resource.Release()
		wrapper.resource = nil
	}

	if wrapper.onRelease != nil {
		wrapper.onRelease()
		wrapper.onRelease = nil
	}

	wrapper.released = true
	l.Push(lua.LTrue)
	return 1
}

func storageListObjects(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireListObjectsYield()
	yield.Storage = wrapper.storage

	if l.Get(2) != lua.LNil {
		optsTable := l.CheckTable(2)
		yield.Options = &csapi.ListObjectsOptions{}

		if prefix := optsTable.RawGetString("prefix"); prefix != lua.LNil {
			yield.Options.Prefix = prefix.String()
		}
		if maxKeys := optsTable.RawGetString("max_keys"); maxKeys != lua.LNil {
			yield.Options.MaxKeys = int(lua.LVAsNumber(maxKeys))
		}
		if token := optsTable.RawGetString("continuation_token"); token != lua.LNil {
			yield.Options.ContinuationToken = token.String()
		}
		if v := optsTable.RawGetString("include_owner"); v != lua.LNil {
			yield.Options.IncludeOwner = lua.LVAsBool(v)
		}
		if v := optsTable.RawGetString("include_versions"); v != lua.LNil {
			yield.Options.IncludeVersions = lua.LVAsBool(v)
		}
	}

	l.Push(yield)
	return -1
}

func storageHeadObject(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireHeadObjectYield()
	yield.Storage = wrapper.storage
	yield.Key = key

	l.Push(yield)
	return -1
}

func storageDownloadObject(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	// V1 compatible: 3rd arg is writer (io.Writer userdata)
	ud := l.CheckUserData(3)
	writer, ok := ud.Value.(io.Writer)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "third argument must implement io.Writer").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireDownloadObjectYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.Writer = writer

	// V1 compatible: 4th arg is options
	if l.Get(4) != lua.LNil {
		optsTable := l.CheckTable(4)
		yield.Options = &csapi.DownloadOptions{}

		if rang := optsTable.RawGetString("range"); rang != lua.LNil {
			yield.Options.Range = rang.String()
		}
		if v := optsTable.RawGetString("if_match"); v != lua.LNil {
			yield.Options.IfMatch = v.String()
		}
		if v := optsTable.RawGetString("if_none_match"); v != lua.LNil {
			yield.Options.IfNoneMatch = v.String()
		}
	}

	l.Push(yield)
	return -1
}

func storageUploadObject(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	content := l.Get(3)
	if content == lua.LNil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "content is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireUploadObjectYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.Content = content

	if l.Get(4) != lua.LNil {
		optsTable := l.CheckTable(4)
		uo := &csapi.UploadOptions{}

		if v := optsTable.RawGetString("content_type"); v != lua.LNil {
			uo.ContentType = v.String()
		}
		if v := optsTable.RawGetString("cache_control"); v != lua.LNil {
			uo.CacheControl = v.String()
		}
		if v := optsTable.RawGetString("content_disposition"); v != lua.LNil {
			uo.ContentDisposition = v.String()
		}
		if v := optsTable.RawGetString("content_encoding"); v != lua.LNil {
			uo.ContentEncoding = v.String()
		}
		if v := optsTable.RawGetString("if_match"); v != lua.LNil {
			uo.IfMatch = v.String()
		}
		if v := optsTable.RawGetString("if_none_match"); v != lua.LNil {
			uo.IfNoneMatch = v.String()
		}
		// only_if_absent is a Lua-friendly alias for if_none_match = "*".
		// When true it wins over an explicit if_none_match string.
		if v := optsTable.RawGetString("only_if_absent"); v != lua.LNil && lua.LVAsBool(v) {
			uo.IfNoneMatch = "*"
		}
		if v := optsTable.RawGetString("metadata"); v != lua.LNil {
			if mt, ok := v.(*lua.LTable); ok {
				uo.Metadata = make(map[string]string, mt.Len())
				mt.ForEach(func(k, mv lua.LValue) {
					if ks, kok := k.(lua.LString); kok {
						uo.Metadata[string(ks)] = mv.String()
					}
				})
			}
		}
		if v := optsTable.RawGetString("headers"); v != lua.LNil {
			if ht, ok := v.(*lua.LTable); ok {
				uo.Headers = make(map[string]string, ht.Len())
				ht.ForEach(func(k, hv lua.LValue) {
					if ks, kok := k.(lua.LString); kok {
						uo.Headers[string(ks)] = hv.String()
					}
				})
			}
		}
		yield.Options = uo
	}

	l.Push(yield)
	return -1
}

func storageDeleteObjects(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	keysTable := l.CheckTable(2)
	keys := make([]string, keysTable.Len())
	keysTable.ForEach(func(idx, value lua.LValue) {
		if idx.Type() == lua.LTNumber {
			i := int(lua.LVAsNumber(idx)) - 1
			if i >= 0 && i < len(keys) {
				keys[i] = value.String()
			}
		}
	})

	yield := AcquireDeleteObjectsYield()
	yield.Storage = wrapper.storage
	yield.Keys = keys

	l.Push(yield)
	return -1
}

func storagePresignedGetURL(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquirePresignedGetURLYield()
	yield.Storage = wrapper.storage
	yield.Key = key

	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		if exp := optsTable.RawGetString("expiration"); exp != lua.LNil {
			yield.Expiration = int64(lua.LVAsNumber(exp))
		}
	}

	l.Push(yield)
	return -1
}

func storagePresignedPutURL(l *lua.LState) int {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return 0
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquirePresignedPutURLYield()
	yield.Storage = wrapper.storage
	yield.Key = key

	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		if exp := optsTable.RawGetString("expiration"); exp != lua.LNil {
			yield.Expiration = int64(lua.LVAsNumber(exp))
		}
		if ct := optsTable.RawGetString("content_type"); ct != lua.LNil {
			yield.ContentType = ct.String()
		}
		if cl := optsTable.RawGetString("content_length"); cl != lua.LNil {
			yield.ContentLength = int64(lua.LVAsNumber(cl))
		}
	}

	l.Push(yield)
	return -1
}
