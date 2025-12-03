package pack

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrDataCorrupted = &Error{
		kind:    apierror.KindInternal,
		message: "data hash mismatch: data corrupted",
	}
	ErrNegativePosition = &Error{
		kind:    apierror.KindInvalid,
		message: "negative position",
	}
	ErrIsDirectory = &Error{
		kind:    apierror.KindInvalid,
		message: "is a directory",
	}
	ErrNonChunkedBlobUnsupported = &Error{
		kind:    apierror.KindInvalid,
		message: "blob has no chunks: non-chunked blob format not supported by current writer",
	}
	ErrReaderMustImplementInterfaces = &Error{
		kind:    apierror.KindInvalid,
		message: "reader must implement io.ReaderAt or both io.ReadSeeker and io.ReaderAt",
	}
)

func NewFrameNotFoundError(frameIdx int) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("frame not found: %d", frameIdx),
	}
}

func NewDecompressError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decompress file data",
		cause:   cause,
	}
}

func NewInvalidWhenceError(whence int) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid whence: %d", whence),
	}
}

func NewNegativeOffsetError() *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "negative offset",
	}
}

func NewChunkFrameNotFoundError(frameIdx int) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("chunk frame not found: %d", frameIdx),
	}
}

func NewReadHeaderError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read header",
		cause:   cause,
	}
}

func NewInvalidMagicError(magic string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid magic: %q", magic),
	}
}

func NewUnsupportedVersionError(version uint32) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("unsupported version: %d", version),
	}
}

func NewWriteHeaderError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to write header",
		cause:   cause,
	}
}

func NewSeekToFooterError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to seek to footer",
		cause:   cause,
	}
}

func NewReadFooterError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read footer",
		cause:   cause,
	}
}

func NewWriteFooterError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to write footer",
		cause:   cause,
	}
}

func NewResetZstdDecoderError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to reset zstd decoder",
		cause:   cause,
	}
}

func NewInvalidResourceIDError(id interface{}) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("resource ID is invalid: %v", id),
	}
}

func NewResourceFilesystemNilError(id string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("resource filesystem is nil for ID: %s", id),
	}
}

func NewDataSizeExceedsMaxError(dataSize, maxSize uint64) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("data size %d exceeds maximum %d", dataSize, maxSize),
	}
}

func NewReadDataError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read data for validation",
		cause:   cause,
	}
}

func NewReadTOCError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read TOC",
		cause:   cause,
	}
}

func NewDecompressTOCError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decompress TOC",
		cause:   cause,
	}
}

func NewDecodeTOCError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decode TOC",
		cause:   cause,
	}
}

func NewReadMetadataFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read metadata frame",
		cause:   cause,
	}
}

func NewDecodeMetadataError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decode metadata",
		cause:   cause,
	}
}

func NewReadEntriesFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read entries frame",
		cause:   cause,
	}
}

func NewDecodeEntriesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decode entries",
		cause:   cause,
	}
}

func NewInvalidEntryFormatError(index int) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid entry format at index %d", index),
	}
}

func NewResourceNotTreeError(id string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("resource %s is not a tree", id),
	}
}

func NewResourceNotBlobError(id string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("resource %s is not a blob", id),
	}
}

func NewResourceNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("resource not found: %s", id),
	}
}

func NewReadResourceFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read resource frame",
		cause:   cause,
	}
}

func NewDecodeTreeResourceError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decode tree resource",
		cause:   cause,
	}
}

func NewDecodeBlobResourceError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to decode blob resource",
		cause:   cause,
	}
}

func NewUnknownResourceTypeError(resourceType string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("unknown resource type: %s", resourceType),
	}
}

func NewReadFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read frame",
		cause:   cause,
	}
}

func NewReadFrameDataError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read frame data",
		cause:   cause,
	}
}

func NewGetMetadataError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to get metadata",
		cause:   cause,
	}
}

func NewGetEntriesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to get entries",
		cause:   cause,
	}
}

func NewNormalizeEntryError(index int, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to normalize entry %d", index),
		cause:   cause,
	}
}

func NewCreateMetadataFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create metadata frame",
		cause:   cause,
	}
}

func NewCreateEntriesFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create entries frame",
		cause:   cause,
	}
}

func NewProcessFilesystemError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to process filesystem",
		cause:   cause,
	}
}

func NewCreateResourceFrameError(resourceID string, cause error) *Error {
	msg := "failed to create resource frame"
	if resourceID != "" {
		msg = fmt.Sprintf("failed to create resource frame for %s", resourceID)
	}
	return &Error{
		kind:    apierror.KindInternal,
		message: msg,
		cause:   cause,
	}
}

func NewProcessResourceFilesystemError(resourceID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to process filesystem %s", resourceID),
		cause:   cause,
	}
}

func NewStatFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to stat %s", filePath),
		cause:   cause,
	}
}

func NewOpenFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to open %s", filePath),
		cause:   cause,
	}
}

func NewReadFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to read %s", filePath),
		cause:   cause,
	}
}

func NewCreateZstdWriterError(filePath string, cause error) *Error {
	msg := "failed to create zstd writer"
	if filePath != "" {
		msg = fmt.Sprintf("failed to create zstd writer for %s", filePath)
	}
	return &Error{
		kind:    apierror.KindInternal,
		message: msg,
		cause:   cause,
	}
}

func NewCompressFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to compress %s", filePath),
		cause:   cause,
	}
}

func NewCloseZstdWriterError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to close zstd writer for %s", filePath),
		cause:   cause,
	}
}

func NewMsgpackEncodeError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to msgpack encode",
		cause:   cause,
	}
}

func NewZstdCompressError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to zstd compress",
		cause:   cause,
	}
}

func NewInsufficientFramesError(got, need int) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("insufficient frames: got %d, need at least %d", got, need),
	}
}

func NewWriteFrameError(frameIndex int, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to write frame %d", frameIndex),
		cause:   cause,
	}
}

func NewCreateTOCFrameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create TOC frame",
		cause:   cause,
	}
}

func NewWriteTOCError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to write TOC",
		cause:   cause,
	}
}

func NewTranscodePayloadError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to transcode payload",
		cause:   cause,
	}
}
