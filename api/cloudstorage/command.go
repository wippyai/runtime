// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"io"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("cloudstorage",
		ListObjects, DownloadObject, UploadObject,
		DeleteObjects, PresignedGetURL, PresignedPutURL,
		HeadObject,
	)
}

// Command IDs for cloud storage operations.
const (
	ListObjects     dispatcher.CommandID = 160
	DownloadObject  dispatcher.CommandID = 161
	UploadObject    dispatcher.CommandID = 162
	DeleteObjects   dispatcher.CommandID = 163
	PresignedGetURL dispatcher.CommandID = 164
	PresignedPutURL dispatcher.CommandID = 165
	HeadObject      dispatcher.CommandID = 166
)

// ListObjectsCmd lists objects in cloud storage.
type ListObjectsCmd struct {
	Storage Storage
	Options *ListObjectsOptions
}

var listObjectsCmdPool = sync.Pool{New: func() any { return &ListObjectsCmd{} }}

// AcquireListObjectsCmd returns a pooled ListObjectsCmd.
func AcquireListObjectsCmd() *ListObjectsCmd          { return listObjectsCmdPool.Get().(*ListObjectsCmd) }
func (c *ListObjectsCmd) CmdID() dispatcher.CommandID { return ListObjects }
func (c *ListObjectsCmd) Release() {
	c.Storage = nil
	c.Options = nil
	listObjectsCmdPool.Put(c)
}

// ListObjectsResponse contains list objects results.
type ListObjectsResponse struct {
	Result *ListObjectsResult
	Error  error
}

// DownloadObjectCmd downloads an object from cloud storage.
type DownloadObjectCmd struct {
	Storage Storage
	Writer  io.Writer
	Options *DownloadOptions
	Key     string
}

var downloadObjectCmdPool = sync.Pool{New: func() any { return &DownloadObjectCmd{} }}

// AcquireDownloadObjectCmd returns a pooled DownloadObjectCmd.
func AcquireDownloadObjectCmd() *DownloadObjectCmd {
	return downloadObjectCmdPool.Get().(*DownloadObjectCmd)
}
func (c *DownloadObjectCmd) CmdID() dispatcher.CommandID { return DownloadObject }
func (c *DownloadObjectCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Writer = nil
	c.Options = nil
	downloadObjectCmdPool.Put(c)
}

// DownloadObjectResponse contains download results.
type DownloadObjectResponse struct {
	Error error
}

// UploadObjectCmd uploads an object to cloud storage.
type UploadObjectCmd struct {
	Storage Storage
	Reader  io.Reader
	Options *UploadOptions
	Key     string
}

var uploadObjectCmdPool = sync.Pool{New: func() any { return &UploadObjectCmd{} }}

// AcquireUploadObjectCmd returns a pooled UploadObjectCmd.
func AcquireUploadObjectCmd() *UploadObjectCmd         { return uploadObjectCmdPool.Get().(*UploadObjectCmd) }
func (c *UploadObjectCmd) CmdID() dispatcher.CommandID { return UploadObject }
func (c *UploadObjectCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Reader = nil
	c.Options = nil
	uploadObjectCmdPool.Put(c)
}

// UploadObjectResponse contains upload results.
type UploadObjectResponse struct {
	Error error
}

// DeleteObjectsCmd deletes objects from cloud storage.
type DeleteObjectsCmd struct {
	Storage Storage
	Keys    []string
}

var deleteObjectsCmdPool = sync.Pool{New: func() any { return &DeleteObjectsCmd{} }}

// AcquireDeleteObjectsCmd returns a pooled DeleteObjectsCmd.
func AcquireDeleteObjectsCmd() *DeleteObjectsCmd {
	return deleteObjectsCmdPool.Get().(*DeleteObjectsCmd)
}
func (c *DeleteObjectsCmd) CmdID() dispatcher.CommandID { return DeleteObjects }
func (c *DeleteObjectsCmd) Release() {
	c.Storage = nil
	c.Keys = nil
	deleteObjectsCmdPool.Put(c)
}

// DeleteObjectsResponse contains delete results.
type DeleteObjectsResponse struct {
	Error error
}

// PresignedGetURLCmd generates a presigned GET URL.
type PresignedGetURLCmd struct {
	Storage    Storage
	Key        string
	Expiration time.Duration
}

var presignedGetURLCmdPool = sync.Pool{New: func() any { return &PresignedGetURLCmd{} }}

// AcquirePresignedGetURLCmd returns a pooled PresignedGetURLCmd.
func AcquirePresignedGetURLCmd() *PresignedGetURLCmd {
	return presignedGetURLCmdPool.Get().(*PresignedGetURLCmd)
}
func (c *PresignedGetURLCmd) CmdID() dispatcher.CommandID { return PresignedGetURL }
func (c *PresignedGetURLCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Expiration = 0
	presignedGetURLCmdPool.Put(c)
}

// PresignedGetURLResponse contains presigned GET URL results.
type PresignedGetURLResponse struct {
	Error error
	URL   string
}

// PresignedPutURLCmd generates a presigned PUT URL.
type PresignedPutURLCmd struct {
	Storage       Storage
	Key           string
	ContentType   string
	Expiration    time.Duration
	ContentLength int64
}

var presignedPutURLCmdPool = sync.Pool{New: func() any { return &PresignedPutURLCmd{} }}

// AcquirePresignedPutURLCmd returns a pooled PresignedPutURLCmd.
func AcquirePresignedPutURLCmd() *PresignedPutURLCmd {
	return presignedPutURLCmdPool.Get().(*PresignedPutURLCmd)
}
func (c *PresignedPutURLCmd) CmdID() dispatcher.CommandID { return PresignedPutURL }
func (c *PresignedPutURLCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Expiration = 0
	c.ContentType = ""
	c.ContentLength = 0
	presignedPutURLCmdPool.Put(c)
}

// PresignedPutURLResponse contains presigned PUT URL results.
type PresignedPutURLResponse struct {
	Error error
	URL   string
}

// HeadObjectCmd fetches full metadata for a single object.
type HeadObjectCmd struct {
	Storage Storage
	Key     string
}

var headObjectCmdPool = sync.Pool{New: func() any { return &HeadObjectCmd{} }}

// AcquireHeadObjectCmd returns a pooled HeadObjectCmd.
func AcquireHeadObjectCmd() *HeadObjectCmd           { return headObjectCmdPool.Get().(*HeadObjectCmd) }
func (c *HeadObjectCmd) CmdID() dispatcher.CommandID { return HeadObject }
func (c *HeadObjectCmd) Release() {
	c.Storage = nil
	c.Key = ""
	headObjectCmdPool.Put(c)
}

// HeadObjectResponse contains head_object results.
type HeadObjectResponse struct {
	Result *HeadObjectResult
	Error  error
}
