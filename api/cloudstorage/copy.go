// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

// ErrCopyUnsupported is returned when the provider cannot copy objects server-side.
var ErrCopyUnsupported = errors.New("cloudstorage: server-side copy not supported by this provider")

// MetadataDirective selects whether the destination keeps the source object's
// metadata or takes the metadata supplied in CopyOptions.
type MetadataDirective string

const (
	// MetadataDirectiveCopy carries the source metadata over to the destination.
	MetadataDirectiveCopy MetadataDirective = "COPY"
	// MetadataDirectiveReplace replaces the source metadata with CopyOptions values.
	MetadataDirectiveReplace MetadataDirective = "REPLACE"
)

type (
	// CopyOptions contains options for server-side object copy.
	CopyOptions struct {
		// Metadata is the destination user metadata. Requires MetadataDirectiveReplace.
		Metadata map[string]string
		// MetadataDirective defaults to MetadataDirectiveCopy when empty.
		MetadataDirective MetadataDirective
		// ContentType is the destination Content-Type. Requires MetadataDirectiveReplace.
		ContentType string
		// IfMatch copies only if the source object's current ETag matches.
		IfMatch string
		// IfNoneMatch is a destination condition; "*" copies only if the destination does not exist.
		IfNoneMatch string
	}

	// CopyResult describes the destination object after a copy.
	CopyResult struct {
		LastModified time.Time
		ETag         string
	}

	// CopyStorage is implemented by providers that copy objects server-side.
	CopyStorage interface {
		Storage

		// CopyObject copies srcKey to dstKey within the storage without moving object data through the caller.
		CopyObject(ctx context.Context, srcKey, dstKey string, opts *CopyOptions) (*CopyResult, error)
	}
)

// CopyObjectCmd copies an object within cloud storage.
type CopyObjectCmd struct {
	Storage Storage
	Options *CopyOptions
	SrcKey  string
	DstKey  string
}

var copyObjectCmdPool = sync.Pool{New: func() any { return &CopyObjectCmd{} }}

// AcquireCopyObjectCmd returns a pooled CopyObjectCmd.
func AcquireCopyObjectCmd() *CopyObjectCmd           { return copyObjectCmdPool.Get().(*CopyObjectCmd) }
func (c *CopyObjectCmd) CmdID() dispatcher.CommandID { return CopyObject }
func (c *CopyObjectCmd) Release() {
	c.Storage = nil
	c.SrcKey = ""
	c.DstKey = ""
	c.Options = nil
	copyObjectCmdPool.Put(c)
}

// CopyObjectResponse contains copy_object results.
type CopyObjectResponse struct {
	Result *CopyResult
	Error  error
}
