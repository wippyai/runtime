// Package csapi provides cloud storage command types for the dispatcher system.
package csapi

import (
	"io"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("cloudstorage",
		CmdListObjects, CmdDownloadObject, CmdUploadObject,
		CmdDeleteObjects, CmdPresignedGetURL, CmdPresignedPutURL,
	)
}

// Command IDs for cloud storage operations.
// Range 200-249 is reserved for cloud storage commands.
const (
	CmdListObjects     dispatcher.CommandID = 200
	CmdDownloadObject  dispatcher.CommandID = 201
	CmdUploadObject    dispatcher.CommandID = 202
	CmdDeleteObjects   dispatcher.CommandID = 203
	CmdPresignedGetURL dispatcher.CommandID = 204
	CmdPresignedPutURL dispatcher.CommandID = 205
)

// ListObjectsCmd lists objects in cloud storage.
type ListObjectsCmd struct {
	Storage cloudstorage.Storage
	Options *cloudstorage.ListObjectsOptions
}

var listObjectsCmdPool = sync.Pool{New: func() any { return &ListObjectsCmd{} }}

func AcquireListObjectsCmd() *ListObjectsCmd          { return listObjectsCmdPool.Get().(*ListObjectsCmd) }
func (c *ListObjectsCmd) CmdID() dispatcher.CommandID { return CmdListObjects }
func (c *ListObjectsCmd) Release() {
	c.Storage = nil
	c.Options = nil
	listObjectsCmdPool.Put(c)
}

// ListObjectsResponse contains list objects results.
type ListObjectsResponse struct {
	Result *cloudstorage.ListObjectsResult
	Error  error
}

// DownloadObjectCmd downloads an object from cloud storage.
type DownloadObjectCmd struct {
	Storage cloudstorage.Storage
	Key     string
	Writer  io.Writer
	Options *cloudstorage.DownloadOptions
}

var downloadObjectCmdPool = sync.Pool{New: func() any { return &DownloadObjectCmd{} }}

func AcquireDownloadObjectCmd() *DownloadObjectCmd {
	return downloadObjectCmdPool.Get().(*DownloadObjectCmd)
}
func (c *DownloadObjectCmd) CmdID() dispatcher.CommandID { return CmdDownloadObject }
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
	Storage cloudstorage.Storage
	Key     string
	Reader  io.Reader
}

var uploadObjectCmdPool = sync.Pool{New: func() any { return &UploadObjectCmd{} }}

func AcquireUploadObjectCmd() *UploadObjectCmd         { return uploadObjectCmdPool.Get().(*UploadObjectCmd) }
func (c *UploadObjectCmd) CmdID() dispatcher.CommandID { return CmdUploadObject }
func (c *UploadObjectCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Reader = nil
	uploadObjectCmdPool.Put(c)
}

// UploadObjectResponse contains upload results.
type UploadObjectResponse struct {
	Error error
}

// DeleteObjectsCmd deletes objects from cloud storage.
type DeleteObjectsCmd struct {
	Storage cloudstorage.Storage
	Keys    []string
}

var deleteObjectsCmdPool = sync.Pool{New: func() any { return &DeleteObjectsCmd{} }}

func AcquireDeleteObjectsCmd() *DeleteObjectsCmd {
	return deleteObjectsCmdPool.Get().(*DeleteObjectsCmd)
}
func (c *DeleteObjectsCmd) CmdID() dispatcher.CommandID { return CmdDeleteObjects }
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
	Storage    cloudstorage.Storage
	Key        string
	Expiration time.Duration
}

var presignedGetURLCmdPool = sync.Pool{New: func() any { return &PresignedGetURLCmd{} }}

func AcquirePresignedGetURLCmd() *PresignedGetURLCmd {
	return presignedGetURLCmdPool.Get().(*PresignedGetURLCmd)
}
func (c *PresignedGetURLCmd) CmdID() dispatcher.CommandID { return CmdPresignedGetURL }
func (c *PresignedGetURLCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Expiration = 0
	presignedGetURLCmdPool.Put(c)
}

// PresignedGetURLResponse contains presigned GET URL results.
type PresignedGetURLResponse struct {
	URL   string
	Error error
}

// PresignedPutURLCmd generates a presigned PUT URL.
type PresignedPutURLCmd struct {
	Storage       cloudstorage.Storage
	Key           string
	Expiration    time.Duration
	ContentType   string
	ContentLength int64
}

var presignedPutURLCmdPool = sync.Pool{New: func() any { return &PresignedPutURLCmd{} }}

func AcquirePresignedPutURLCmd() *PresignedPutURLCmd {
	return presignedPutURLCmdPool.Get().(*PresignedPutURLCmd)
}
func (c *PresignedPutURLCmd) CmdID() dispatcher.CommandID { return CmdPresignedPutURL }
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
	URL   string
	Error error
}
