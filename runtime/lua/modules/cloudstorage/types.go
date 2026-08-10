// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ListObjectsOptions type
var listObjectsOptionsType = typ.NewRecord().
	OptField("prefix", typ.String).
	OptField("max_keys", typ.Number).
	OptField("continuation_token", typ.String).
	OptField("include_owner", typ.Boolean).
	OptField("include_versions", typ.Boolean).
	Build()

// Owner type
var ownerType = typ.NewRecord().
	Field("id", typ.String).
	Field("display_name", typ.String).
	Build()

// ListObjectsResult type
var listObjectsResultType = typ.NewRecord().
	Field("objects", typ.NewArray(typ.Any)).
	OptField("next_continuation_token", typ.String).
	Field("is_truncated", typ.Boolean).
	Build()

// HeadObjectResult type
var headObjectResultType = typ.NewRecord().
	Field("size", typ.Number).
	Field("etag", typ.String).
	Field("content_type", typ.String).
	Field("cache_control", typ.String).
	Field("content_disposition", typ.String).
	Field("content_encoding", typ.String).
	Field("storage_class", typ.String).
	OptField("version_id", typ.String).
	OptField("last_modified", typ.Number).
	Field("metadata", typ.Any).
	Field("headers", typ.Any).
	Build()

// DownloadOptions type
var downloadOptionsType = typ.NewRecord().
	OptField("range", typ.String).
	OptField("if_match", typ.String).
	OptField("if_none_match", typ.String).
	Build()

// UploadOptions type
var uploadOptionsType = typ.NewRecord().
	OptField("content_type", typ.String).
	OptField("cache_control", typ.String).
	OptField("content_disposition", typ.String).
	OptField("content_encoding", typ.String).
	OptField("metadata", typ.Any).
	OptField("headers", typ.Any).
	OptField("if_match", typ.String).
	OptField("if_none_match", typ.String).
	OptField("only_if_absent", typ.Boolean).
	Build()

// PresignedURLOptions type
var presignedURLOptionsType = typ.NewRecord().
	OptField("expiration", typ.Number).
	OptField("content_type", typ.String).
	OptField("content_length", typ.Number).
	Build()

// CreateMultipartUploadOptions type
var createMultipartUploadOptionsType = typ.NewRecord().
	OptField("content_type", typ.String).
	OptField("cache_control", typ.String).
	OptField("content_disposition", typ.String).
	OptField("content_encoding", typ.String).
	OptField("metadata", typ.Any).
	OptField("headers", typ.Any).
	Build()

// CreateMultipartUploadResult type
var createMultipartUploadResultType = typ.NewRecord().
	Field("upload_id", typ.String).
	Build()

// PresignedPartURLsOptions type
var presignedPartURLsOptionsType = typ.NewRecord().
	OptField("parts", typ.NewArray(typ.Number)).
	OptField("count", typ.Number).
	OptField("expiration", typ.Number).
	Build()

// PresignedPartURL type
var presignedPartURLType = typ.NewRecord().
	Field("part_number", typ.Number).
	Field("url", typ.String).
	Build()

// CompletedPart type
var completedPartType = typ.NewRecord().
	Field("part_number", typ.Number).
	Field("etag", typ.String).
	Build()

// CompleteMultipartUploadResult type
var completeMultipartUploadResultType = typ.NewRecord().
	Field("etag", typ.String).
	OptField("version_id", typ.String).
	OptField("location", typ.String).
	Build()

// OpenReaderOptions type
var openReaderOptionsType = typ.NewRecord().
	OptField("block_size", typ.Number).
	OptField("cache_blocks", typ.Number).
	Build()

// Reader type
var readerType = typ.NewInterface("cloudstorage.Reader", []typ.Method{
	{Name: "size", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
	{Name: "key", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

// Storage type
var storageType = typ.NewInterface("cloudstorage.Storage", []typ.Method{
	{Name: "list_objects", Type: typ.Func().Param("self", typ.Self).OptParam("options", listObjectsOptionsType).Returns(listObjectsResultType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "head_object", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(headObjectResultType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "download_object", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("dest", typ.Any).OptParam("options", downloadOptionsType).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "upload_object", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("source", typ.Any).OptParam("options", uploadOptionsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "delete_objects", Type: typ.Func().Param("self", typ.Self).Param("keys", typ.NewArray(typ.String)).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "presigned_get_url", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", presignedURLOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "presigned_put_url", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", presignedURLOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "create_multipart_upload", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", createMultipartUploadOptionsType).Returns(createMultipartUploadResultType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "presigned_part_urls", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("upload_id", typ.String).Param("options", presignedPartURLsOptionsType).Returns(typ.NewArray(presignedPartURLType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "complete_multipart_upload", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("upload_id", typ.String).Param("parts", typ.NewArray(completedPartType)).Returns(completeMultipartUploadResultType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "abort_multipart_upload", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("upload_id", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "open_reader", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", openReaderOptionsType).Returns(readerType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})

// ModuleTypes returns the type manifest for the cloudstorage module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("cloudstorage")

	m.DefineType("Storage", storageType)
	m.DefineType("Reader", readerType)
	m.DefineType("ListObjectsOptions", listObjectsOptionsType)
	m.DefineType("ListObjectsResult", listObjectsResultType)
	m.DefineType("Owner", ownerType)
	m.DefineType("HeadObjectResult", headObjectResultType)
	m.DefineType("DownloadOptions", downloadOptionsType)
	m.DefineType("UploadOptions", uploadOptionsType)
	m.DefineType("PresignedURLOptions", presignedURLOptionsType)
	m.DefineType("CreateMultipartUploadOptions", createMultipartUploadOptionsType)
	m.DefineType("CreateMultipartUploadResult", createMultipartUploadResultType)
	m.DefineType("PresignedPartURLsOptions", presignedPartURLsOptionsType)
	m.DefineType("PresignedPartURL", presignedPartURLType)
	m.DefineType("CompletedPart", completedPartType)
	m.DefineType("CompleteMultipartUploadResult", completeMultipartUploadResultType)
	m.DefineType("OpenReaderOptions", openReaderOptionsType)

	moduleType := typ.NewInterface("cloudstorage", []typ.Method{
		{Name: "get", Type: typ.Func().Param("name", typ.String).Returns(storageType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
