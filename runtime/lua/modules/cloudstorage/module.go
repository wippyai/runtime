// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
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
		value.RegisterTypeMethods(nil, readerTypeName, readerMetamethods, readerMethods)

		return mod, []luaapi.YieldType{
			{Sample: &ListObjectsYield{}, CmdID: csapi.ListObjects},
			{Sample: &DownloadObjectYield{}, CmdID: csapi.DownloadObject},
			{Sample: &UploadObjectYield{}, CmdID: csapi.UploadObject},
			{Sample: &DeleteObjectsYield{}, CmdID: csapi.DeleteObjects},
			{Sample: &PresignedGetURLYield{}, CmdID: csapi.PresignedGetURL},
			{Sample: &PresignedPutURLYield{}, CmdID: csapi.PresignedPutURL},
			{Sample: &HeadObjectYield{}, CmdID: csapi.HeadObject},
			{Sample: &CreateMultipartUploadYield{}, CmdID: csapi.CreateMultipartUpload},
			{Sample: &PresignedPartURLsYield{}, CmdID: csapi.PresignedUploadPartURLs},
			{Sample: &CompleteMultipartUploadYield{}, CmdID: csapi.CompleteMultipartUpload},
			{Sample: &AbortMultipartUploadYield{}, CmdID: csapi.AbortMultipartUpload},
			{Sample: &OpenReaderYield{}, CmdID: csapi.OpenReader},
		}
	},
	Types: ModuleTypes,
}

// storageWrapper wraps a cloud storage instance with resource tracking.
type storageWrapper struct {
	storage  csapi.Storage
	resource resource.Resource[any]
	released bool
}

var storageMethods = map[string]lua.LGoFunc{
	"list_objects":              storageListObjects,
	"head_object":               storageHeadObject,
	"download_object":           storageDownloadObject,
	"upload_object":             storageUploadObject,
	"delete_objects":            storageDeleteObjects,
	"presigned_get_url":         storagePresignedGetURL,
	"presigned_put_url":         storagePresignedPutURL,
	"create_multipart_upload":   storageCreateMultipartUpload,
	"presigned_part_urls":       storagePresignedPartURLs,
	"complete_multipart_upload": storageCompleteMultipartUpload,
	"abort_multipart_upload":    storageAbortMultipartUpload,
	"open_reader":               storageOpenReader,
	"release":                   storageRelease,
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

	res, storageRes, err := rtresource.AcquireRegistryResource(ctx, reg, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.NotFound).WithRetryable(false))
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
		storage:  csRes,
		resource: res,
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

	if ud, ok := content.(*lua.LUserData); ok {
		if _, direct := ud.Value.(io.Reader); !direct {
			if rp, isProvider := ud.Value.(rtresource.ReaderProvider); isProvider {
				r, rerr := rp.GetReader(l.Context())
				if rerr != nil {
					ReleaseUploadObjectYield(yield)
					l.Push(lua.LNil)
					l.Push(lua.WrapErrorWithLua(l, rerr, "failed to get reader").WithKind(lua.Internal).WithRetryable(false))
					return 2
				}
				yield.ContentReader = r
			}
		}
	}

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

func checkStorageAndKey(l *lua.LState) (*storageWrapper, string, bool) {
	wrapper := checkStorage(l, 1)
	if wrapper == nil {
		return nil, "", false
	}

	if wrapper.released {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "storage has been released").WithKind(lua.Invalid).WithRetryable(false))
		return nil, "", false
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "key is required").WithKind(lua.Invalid).WithRetryable(false))
		return nil, "", false
	}

	return wrapper, key, true
}

func stringMap(v lua.LValue) map[string]string {
	t, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := make(map[string]string, t.Len())
	t.ForEach(func(k, mv lua.LValue) {
		if ks, kok := k.(lua.LString); kok {
			out[string(ks)] = mv.String()
		}
	})
	return out
}

func storageCreateMultipartUpload(l *lua.LState) int {
	wrapper, key, ok := checkStorageAndKey(l)
	if !ok {
		return 2
	}

	yield := AcquireCreateMultipartUploadYield()
	yield.Storage = wrapper.storage
	yield.Key = key

	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		mo := &csapi.CreateMultipartUploadOptions{}

		if v := optsTable.RawGetString("content_type"); v != lua.LNil {
			mo.ContentType = v.String()
		}
		if v := optsTable.RawGetString("cache_control"); v != lua.LNil {
			mo.CacheControl = v.String()
		}
		if v := optsTable.RawGetString("content_disposition"); v != lua.LNil {
			mo.ContentDisposition = v.String()
		}
		if v := optsTable.RawGetString("content_encoding"); v != lua.LNil {
			mo.ContentEncoding = v.String()
		}
		if v := optsTable.RawGetString("metadata"); v != lua.LNil {
			mo.Metadata = stringMap(v)
		}
		if v := optsTable.RawGetString("headers"); v != lua.LNil {
			mo.Headers = stringMap(v)
		}
		yield.Options = mo
	}

	l.Push(yield)
	return -1
}

func storagePresignedPartURLs(l *lua.LState) int {
	wrapper, key, ok := checkStorageAndKey(l)
	if !ok {
		return 2
	}

	uploadID := l.CheckString(3)
	if uploadID == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "upload_id is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	optsTable := l.CheckTable(4)

	var partNumbers []int32
	if v := optsTable.RawGetString("parts"); v != lua.LNil {
		pt, tok := v.(*lua.LTable)
		if !tok {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "parts must be an array of part numbers").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		partNumbers = make([]int32, 0, pt.Len())
		for i := 1; i <= pt.Len(); i++ {
			partNumbers = append(partNumbers, int32(lua.LVAsNumber(pt.RawGetInt(i))))
		}
	} else if v := optsTable.RawGetString("count"); v != lua.LNil {
		count := int(lua.LVAsNumber(v))
		if count < 1 {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "count must be at least 1").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		if count > csapi.MaxPresignPartBatch {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "count exceeds the per-call presign limit").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		partNumbers = make([]int32, count)
		for i := range partNumbers {
			partNumbers[i] = int32(i + 1)
		}
	}

	if len(partNumbers) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "options must set parts (array) or count (number)").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquirePresignedPartURLsYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.UploadID = uploadID
	yield.Options = &csapi.PresignedUploadPartOptions{PartNumbers: partNumbers}

	if exp := optsTable.RawGetString("expiration"); exp != lua.LNil {
		yield.Expiration = int64(lua.LVAsNumber(exp))
	}

	l.Push(yield)
	return -1
}

func storageCompleteMultipartUpload(l *lua.LState) int {
	wrapper, key, ok := checkStorageAndKey(l)
	if !ok {
		return 2
	}

	uploadID := l.CheckString(3)
	if uploadID == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "upload_id is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	partsTable := l.CheckTable(4)
	parts := make([]csapi.CompletedPart, 0, partsTable.Len())
	for i := 1; i <= partsTable.Len(); i++ {
		entry, tok := partsTable.RawGetInt(i).(*lua.LTable)
		if !tok {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "parts must be an array of {part_number, etag} tables").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		parts = append(parts, csapi.CompletedPart{
			PartNumber: int32(lua.LVAsNumber(entry.RawGetString("part_number"))),
			ETag:       entry.RawGetString("etag").String(),
		})
	}
	if len(parts) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "at least one completed part is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireCompleteMultipartUploadYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.UploadID = uploadID
	yield.Parts = parts

	l.Push(yield)
	return -1
}

func storageAbortMultipartUpload(l *lua.LState) int {
	wrapper, key, ok := checkStorageAndKey(l)
	if !ok {
		return 2
	}

	uploadID := l.CheckString(3)
	if uploadID == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "upload_id is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireAbortMultipartUploadYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.UploadID = uploadID

	l.Push(yield)
	return -1
}

func storageOpenReader(l *lua.LState) int {
	wrapper, key, ok := checkStorageAndKey(l)
	if !ok {
		return 2
	}

	var (
		blockSize   int64
		cacheBlocks int
	)
	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		if v := optsTable.RawGetString("block_size"); v != lua.LNil {
			blockSize = int64(lua.LVAsNumber(v))
			if blockSize < minReaderBlockSize || blockSize > maxReaderBlockSize {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "block_size out of range").WithKind(lua.Invalid).WithRetryable(false))
				return 2
			}
		}
		if v := optsTable.RawGetString("cache_blocks"); v != lua.LNil {
			cacheBlocks = int(lua.LVAsNumber(v))
			if cacheBlocks < 1 || cacheBlocks > csapi.MaxReaderCacheBlocks {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "cache_blocks out of range").WithKind(lua.Invalid).WithRetryable(false))
				return 2
			}
		}
	}

	yield := AcquireOpenReaderYield()
	yield.Storage = wrapper.storage
	yield.Key = key
	yield.BlockSize = blockSize
	yield.CacheBlocks = cacheBlocks

	l.Push(yield)
	return -1
}
