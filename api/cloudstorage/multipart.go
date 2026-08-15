// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

var ErrMultipartUnsupported = errors.New("cloudstorage: multipart uploads not supported by this provider")

const MaxPartNumber = 10000
const MaxPresignPartBatch = 1000

type (
	CreateMultipartUploadOptions struct {
		Metadata           map[string]string
		Headers            map[string]string
		ContentType        string
		CacheControl       string
		ContentDisposition string
		ContentEncoding    string
	}

	CreateMultipartUploadResult struct {
		UploadID string
	}

	PresignedUploadPartOptions struct {
		Headers     map[string]string
		PartNumbers []int32
		Expiration  time.Duration
	}

	PresignedPartURL struct {
		URL        string
		PartNumber int32
	}

	CompletedPart struct {
		ETag       string
		PartNumber int32
	}

	CompleteMultipartUploadResult struct {
		ETag      string
		VersionID string
		Location  string
	}

	MultipartStorage interface {
		Storage

		CreateMultipartUpload(ctx context.Context, key string, opts *CreateMultipartUploadOptions) (*CreateMultipartUploadResult, error)

		PresignedUploadPartURLs(ctx context.Context, key, uploadID string, opts *PresignedUploadPartOptions) ([]PresignedPartURL, error)

		CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadResult, error)

		AbortMultipartUpload(ctx context.Context, key, uploadID string) error
	}
)

type CreateMultipartUploadCmd struct {
	Storage Storage
	Options *CreateMultipartUploadOptions
	Key     string
}

var createMultipartUploadCmdPool = sync.Pool{New: func() any { return &CreateMultipartUploadCmd{} }}

func AcquireCreateMultipartUploadCmd() *CreateMultipartUploadCmd {
	return createMultipartUploadCmdPool.Get().(*CreateMultipartUploadCmd)
}
func (c *CreateMultipartUploadCmd) CmdID() dispatcher.CommandID { return CreateMultipartUpload }
func (c *CreateMultipartUploadCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.Options = nil
	createMultipartUploadCmdPool.Put(c)
}

type CreateMultipartUploadResponse struct {
	Result *CreateMultipartUploadResult
	Error  error
}

type PresignedPartURLsCmd struct {
	Storage  Storage
	Options  *PresignedUploadPartOptions
	Key      string
	UploadID string
}

var presignedPartURLsCmdPool = sync.Pool{New: func() any { return &PresignedPartURLsCmd{} }}

func AcquirePresignedPartURLsCmd() *PresignedPartURLsCmd {
	return presignedPartURLsCmdPool.Get().(*PresignedPartURLsCmd)
}
func (c *PresignedPartURLsCmd) CmdID() dispatcher.CommandID { return PresignedUploadPartURLs }
func (c *PresignedPartURLsCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.UploadID = ""
	c.Options = nil
	presignedPartURLsCmdPool.Put(c)
}

type PresignedPartURLsResponse struct {
	Error error
	URLs  []PresignedPartURL
}

type CompleteMultipartUploadCmd struct {
	Storage  Storage
	Key      string
	UploadID string
	Parts    []CompletedPart
}

var completeMultipartUploadCmdPool = sync.Pool{New: func() any { return &CompleteMultipartUploadCmd{} }}

func AcquireCompleteMultipartUploadCmd() *CompleteMultipartUploadCmd {
	return completeMultipartUploadCmdPool.Get().(*CompleteMultipartUploadCmd)
}
func (c *CompleteMultipartUploadCmd) CmdID() dispatcher.CommandID { return CompleteMultipartUpload }
func (c *CompleteMultipartUploadCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.UploadID = ""
	c.Parts = nil
	completeMultipartUploadCmdPool.Put(c)
}

type CompleteMultipartUploadResponse struct {
	Result *CompleteMultipartUploadResult
	Error  error
}

type AbortMultipartUploadCmd struct {
	Storage  Storage
	Key      string
	UploadID string
}

var abortMultipartUploadCmdPool = sync.Pool{New: func() any { return &AbortMultipartUploadCmd{} }}

func AcquireAbortMultipartUploadCmd() *AbortMultipartUploadCmd {
	return abortMultipartUploadCmdPool.Get().(*AbortMultipartUploadCmd)
}
func (c *AbortMultipartUploadCmd) CmdID() dispatcher.CommandID { return AbortMultipartUpload }
func (c *AbortMultipartUploadCmd) Release() {
	c.Storage = nil
	c.Key = ""
	c.UploadID = ""
	abortMultipartUploadCmdPool.Put(c)
}

type AbortMultipartUploadResponse struct {
	Error error
}
