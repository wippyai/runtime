// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/dispatcher"
)

// wrapStorageError translates known cloudstorage errors into structured Lua errors.
// Unknown errors fall back to lua.WrapErrorWithLua.
func wrapStorageError(l *lua.LState, err error, op string) lua.LValue {
	if errors.Is(err, csapi.ErrPreconditionFailed) {
		return lua.NewLuaError(l, "precondition_failed").
			WithKind(lua.Conflict).
			WithRetryable(false)
	}
	if errors.Is(err, csapi.ErrNotFound) {
		return lua.NewLuaError(l, "not_found").
			WithKind(lua.NotFound).
			WithRetryable(false)
	}
	return lua.WrapErrorWithLua(l, err, op)
}

// ListObjectsYield wraps ListObjectsCmd for Lua.
type ListObjectsYield struct {
	*csapi.ListObjectsCmd
}

var listObjectsYieldPool = sync.Pool{New: func() any { return &ListObjectsYield{} }}

func AcquireListObjectsYield() *ListObjectsYield {
	y := listObjectsYieldPool.Get().(*ListObjectsYield)
	y.ListObjectsCmd = csapi.AcquireListObjectsCmd()
	return y
}

func ReleaseListObjectsYield(y *ListObjectsYield) {
	if y.ListObjectsCmd != nil {
		y.ListObjectsCmd.Release()
		y.ListObjectsCmd = nil
	}
	listObjectsYieldPool.Put(y)
}

func (y *ListObjectsYield) String() string                { return "<cloudstorage_list_objects_yield>" }
func (y *ListObjectsYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ListObjectsYield) CmdID() dispatcher.CommandID   { return csapi.ListObjects }
func (y *ListObjectsYield) ToCommand() dispatcher.Command { return y.ListObjectsCmd }
func (y *ListObjectsYield) Release()                      { ReleaseListObjectsYield(y) }

func (y *ListObjectsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, err, "list_objects")}
	}
	resp, ok := data.(csapi.ListObjectsResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, resp.Error, "list_objects")}
	}
	return []lua.LValue{listObjectsResultToLua(l, resp.Result), lua.LNil}
}

func listObjectsResultToLua(l *lua.LState, result *csapi.ListObjectsResult) lua.LValue {
	t := l.CreateTable(0, 3)

	objects := l.CreateTable(len(result.Objects), 0)
	for i, obj := range result.Objects {
		objTbl := l.CreateTable(0, 8)
		objTbl.RawSetString("key", lua.LString(obj.Key))
		objTbl.RawSetString("size", lua.LNumber(obj.Size))
		objTbl.RawSetString("content_type", lua.LString(obj.ContentType))
		objTbl.RawSetString("etag", lua.LString(obj.ETag))
		objTbl.RawSetString("storage_class", lua.LString(obj.StorageClass))
		if !obj.LastModified.IsZero() {
			objTbl.RawSetString("last_modified", lua.LNumber(obj.LastModified.Unix()))
		}
		if obj.VersionID != "" {
			objTbl.RawSetString("version_id", lua.LString(obj.VersionID))
		}
		if obj.Owner != nil {
			ownerTbl := l.CreateTable(0, 2)
			ownerTbl.RawSetString("id", lua.LString(obj.Owner.ID))
			ownerTbl.RawSetString("display_name", lua.LString(obj.Owner.DisplayName))
			objTbl.RawSetString("owner", ownerTbl)
		}
		objects.RawSetInt(i+1, objTbl)
	}
	t.RawSetString("objects", objects)
	t.RawSetString("is_truncated", lua.LBool(result.IsTruncated))
	t.RawSetString("next_continuation_token", lua.LString(result.NextContinuationToken))

	return t
}

// DownloadObjectYield wraps DownloadObjectCmd for Lua.
type DownloadObjectYield struct {
	*csapi.DownloadObjectCmd
	Writer io.Writer
}

var downloadObjectYieldPool = sync.Pool{New: func() any { return &DownloadObjectYield{} }}

func AcquireDownloadObjectYield() *DownloadObjectYield {
	y := downloadObjectYieldPool.Get().(*DownloadObjectYield)
	y.DownloadObjectCmd = csapi.AcquireDownloadObjectCmd()
	return y
}

func ReleaseDownloadObjectYield(y *DownloadObjectYield) {
	if y.DownloadObjectCmd != nil {
		y.DownloadObjectCmd.Release()
		y.DownloadObjectCmd = nil
	}
	y.Writer = nil
	downloadObjectYieldPool.Put(y)
}

func (y *DownloadObjectYield) String() string              { return "<cloudstorage_download_object_yield>" }
func (y *DownloadObjectYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *DownloadObjectYield) CmdID() dispatcher.CommandID { return csapi.DownloadObject }
func (y *DownloadObjectYield) ToCommand() dispatcher.Command {
	y.DownloadObjectCmd.Writer = y.Writer
	return y.DownloadObjectCmd
}
func (y *DownloadObjectYield) Release() { ReleaseDownloadObjectYield(y) }

func (y *DownloadObjectYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, err, "download_object")}
	}
	resp, ok := data.(csapi.DownloadObjectResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, resp.Error, "download_object")}
	}
	return []lua.LValue{lua.LTrue}
}

// UploadObjectYield wraps UploadObjectCmd for Lua.
type UploadObjectYield struct {
	*csapi.UploadObjectCmd
	Content lua.LValue
}

var uploadObjectYieldPool = sync.Pool{New: func() any { return &UploadObjectYield{} }}

func AcquireUploadObjectYield() *UploadObjectYield {
	y := uploadObjectYieldPool.Get().(*UploadObjectYield)
	y.UploadObjectCmd = csapi.AcquireUploadObjectCmd()
	return y
}

func ReleaseUploadObjectYield(y *UploadObjectYield) {
	if y.UploadObjectCmd != nil {
		y.UploadObjectCmd.Release()
		y.UploadObjectCmd = nil
	}
	y.Content = nil
	uploadObjectYieldPool.Put(y)
}

func (y *UploadObjectYield) String() string              { return "<cloudstorage_upload_object_yield>" }
func (y *UploadObjectYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *UploadObjectYield) CmdID() dispatcher.CommandID { return csapi.UploadObject }
func (y *UploadObjectYield) ToCommand() dispatcher.Command {
	switch v := y.Content.(type) {
	case lua.LString:
		y.Reader = bytes.NewReader([]byte(v))
	case *lua.LUserData:
		if r, ok := v.Value.(io.Reader); ok {
			y.Reader = r
		}
	}
	return y.UploadObjectCmd
}
func (y *UploadObjectYield) Release() { ReleaseUploadObjectYield(y) }

func (y *UploadObjectYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, err, "upload_object")}
	}
	resp, ok := data.(csapi.UploadObjectResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, resp.Error, "upload_object")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// DeleteObjectsYield wraps DeleteObjectsCmd for Lua.
type DeleteObjectsYield struct {
	*csapi.DeleteObjectsCmd
}

var deleteObjectsYieldPool = sync.Pool{New: func() any { return &DeleteObjectsYield{} }}

func AcquireDeleteObjectsYield() *DeleteObjectsYield {
	y := deleteObjectsYieldPool.Get().(*DeleteObjectsYield)
	y.DeleteObjectsCmd = csapi.AcquireDeleteObjectsCmd()
	return y
}

func ReleaseDeleteObjectsYield(y *DeleteObjectsYield) {
	if y.DeleteObjectsCmd != nil {
		y.DeleteObjectsCmd.Release()
		y.DeleteObjectsCmd = nil
	}
	deleteObjectsYieldPool.Put(y)
}

func (y *DeleteObjectsYield) String() string                { return "<cloudstorage_delete_objects_yield>" }
func (y *DeleteObjectsYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *DeleteObjectsYield) CmdID() dispatcher.CommandID   { return csapi.DeleteObjects }
func (y *DeleteObjectsYield) ToCommand() dispatcher.Command { return y.DeleteObjectsCmd }
func (y *DeleteObjectsYield) Release()                      { ReleaseDeleteObjectsYield(y) }

func (y *DeleteObjectsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "delete_objects")}
	}
	resp, ok := data.(csapi.DeleteObjectsResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "delete_objects")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// PresignedGetURLYield wraps PresignedGetURLCmd for Lua.
type PresignedGetURLYield struct {
	*csapi.PresignedGetURLCmd
	Expiration int64
}

var presignedGetURLYieldPool = sync.Pool{New: func() any { return &PresignedGetURLYield{} }}

func AcquirePresignedGetURLYield() *PresignedGetURLYield {
	y := presignedGetURLYieldPool.Get().(*PresignedGetURLYield)
	y.PresignedGetURLCmd = csapi.AcquirePresignedGetURLCmd()
	return y
}

func ReleasePresignedGetURLYield(y *PresignedGetURLYield) {
	if y.PresignedGetURLCmd != nil {
		y.PresignedGetURLCmd.Release()
		y.PresignedGetURLCmd = nil
	}
	y.Expiration = 0
	presignedGetURLYieldPool.Put(y)
}

func (y *PresignedGetURLYield) String() string              { return "<cloudstorage_presigned_get_url_yield>" }
func (y *PresignedGetURLYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *PresignedGetURLYield) CmdID() dispatcher.CommandID { return csapi.PresignedGetURL }
func (y *PresignedGetURLYield) ToCommand() dispatcher.Command {
	if y.Expiration > 0 {
		y.PresignedGetURLCmd.Expiration = time.Duration(y.Expiration) * time.Second
	} else {
		y.PresignedGetURLCmd.Expiration = time.Hour
	}
	return y.PresignedGetURLCmd
}
func (y *PresignedGetURLYield) Release() { ReleasePresignedGetURLYield(y) }

func (y *PresignedGetURLYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "presigned_get_url")}
	}
	resp, ok := data.(csapi.PresignedGetURLResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "presigned_get_url")}
	}
	return []lua.LValue{lua.LString(resp.URL), lua.LNil}
}

// PresignedPutURLYield wraps PresignedPutURLCmd for Lua.
type PresignedPutURLYield struct {
	*csapi.PresignedPutURLCmd
	ContentType   string
	Expiration    int64
	ContentLength int64
}

var presignedPutURLYieldPool = sync.Pool{New: func() any { return &PresignedPutURLYield{} }}

func AcquirePresignedPutURLYield() *PresignedPutURLYield {
	y := presignedPutURLYieldPool.Get().(*PresignedPutURLYield)
	y.PresignedPutURLCmd = csapi.AcquirePresignedPutURLCmd()
	return y
}

func ReleasePresignedPutURLYield(y *PresignedPutURLYield) {
	if y.PresignedPutURLCmd != nil {
		y.PresignedPutURLCmd.Release()
		y.PresignedPutURLCmd = nil
	}
	y.Expiration = 0
	y.ContentType = ""
	y.ContentLength = 0
	presignedPutURLYieldPool.Put(y)
}

func (y *PresignedPutURLYield) String() string              { return "<cloudstorage_presigned_put_url_yield>" }
func (y *PresignedPutURLYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *PresignedPutURLYield) CmdID() dispatcher.CommandID { return csapi.PresignedPutURL }
func (y *PresignedPutURLYield) ToCommand() dispatcher.Command {
	if y.Expiration > 0 {
		y.PresignedPutURLCmd.Expiration = time.Duration(y.Expiration) * time.Second
	} else {
		y.PresignedPutURLCmd.Expiration = time.Hour
	}
	y.PresignedPutURLCmd.ContentType = y.ContentType
	y.PresignedPutURLCmd.ContentLength = y.ContentLength
	return y.PresignedPutURLCmd
}
func (y *PresignedPutURLYield) Release() { ReleasePresignedPutURLYield(y) }

func (y *PresignedPutURLYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "presigned_put_url")}
	}
	resp, ok := data.(csapi.PresignedPutURLResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "presigned_put_url")}
	}
	return []lua.LValue{lua.LString(resp.URL), lua.LNil}
}

// HeadObjectYield wraps HeadObjectCmd for Lua.
type HeadObjectYield struct {
	*csapi.HeadObjectCmd
}

var headObjectYieldPool = sync.Pool{New: func() any { return &HeadObjectYield{} }}

func AcquireHeadObjectYield() *HeadObjectYield {
	y := headObjectYieldPool.Get().(*HeadObjectYield)
	y.HeadObjectCmd = csapi.AcquireHeadObjectCmd()
	return y
}

func ReleaseHeadObjectYield(y *HeadObjectYield) {
	if y.HeadObjectCmd != nil {
		y.HeadObjectCmd.Release()
		y.HeadObjectCmd = nil
	}
	headObjectYieldPool.Put(y)
}

func (y *HeadObjectYield) String() string                { return "<cloudstorage_head_object_yield>" }
func (y *HeadObjectYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *HeadObjectYield) CmdID() dispatcher.CommandID   { return csapi.HeadObject }
func (y *HeadObjectYield) ToCommand() dispatcher.Command { return y.HeadObjectCmd }
func (y *HeadObjectYield) Release()                      { ReleaseHeadObjectYield(y) }

func (y *HeadObjectYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, err, "head_object")}
	}
	resp, ok := data.(csapi.HeadObjectResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStorageError(l, resp.Error, "head_object")}
	}
	return []lua.LValue{headObjectResultToLua(l, resp.Result), lua.LNil}
}

func headObjectResultToLua(l *lua.LState, result *csapi.HeadObjectResult) lua.LValue {
	t := l.CreateTable(0, 10)
	t.RawSetString("size", lua.LNumber(result.Size))
	t.RawSetString("etag", lua.LString(result.ETag))
	t.RawSetString("content_type", lua.LString(result.ContentType))
	t.RawSetString("cache_control", lua.LString(result.CacheControl))
	t.RawSetString("content_disposition", lua.LString(result.ContentDisposition))
	t.RawSetString("content_encoding", lua.LString(result.ContentEncoding))
	t.RawSetString("storage_class", lua.LString(result.StorageClass))
	if result.VersionID != "" {
		t.RawSetString("version_id", lua.LString(result.VersionID))
	}
	if !result.LastModified.IsZero() {
		t.RawSetString("last_modified", lua.LNumber(result.LastModified.Unix()))
	}
	metaTbl := l.CreateTable(0, len(result.UserMetadata))
	for k, v := range result.UserMetadata {
		metaTbl.RawSetString(k, lua.LString(v))
	}
	t.RawSetString("metadata", metaTbl)
	headersTbl := l.CreateTable(0, len(result.Headers))
	for k, v := range result.Headers {
		headersTbl.RawSetString(k, lua.LString(v))
	}
	t.RawSetString("headers", headersTbl)
	return t
}
