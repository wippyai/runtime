package pack

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDataCorrupted                 = apierror.New(apierror.Internal, "data hash mismatch: data corrupted").WithRetryable(apierror.False)
	ErrNegativePosition              = apierror.New(apierror.Invalid, "negative position").WithRetryable(apierror.False)
	ErrIsDirectory                   = apierror.New(apierror.Invalid, "is a directory").WithRetryable(apierror.False)
	ErrNonChunkedBlobUnsupported     = apierror.New(apierror.Invalid, "blob has no chunks: non-chunked blob format not supported by current writer").WithRetryable(apierror.False)
	ErrReaderMustImplementInterfaces = apierror.New(apierror.Invalid, "reader must implement io.ReaderAt or both io.ReadSeeker and io.ReaderAt").WithRetryable(apierror.False)
)

func NewFrameNotFoundError(frameIdx int) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("frame not found: %d", frameIdx))
}

func NewDecompressError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decompress file data").WithCause(cause)
}

func NewInvalidWhenceError(whence int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid whence: %d", whence))
}

func NewNegativeOffsetError() apierror.Error {
	return apierror.New(apierror.Invalid, "negative offset")
}

func NewChunkFrameNotFoundError(frameIdx int) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("chunk frame not found: %d", frameIdx))
}

func NewReadHeaderError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read header").WithCause(cause)
}

func NewInvalidMagicError(magic string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid magic: %q", magic))
}

func NewUnsupportedVersionError(version uint32) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported version: %d", version))
}

func NewWriteHeaderError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to write header").WithCause(cause)
}

func NewSeekToFooterError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to seek to footer").WithCause(cause)
}

func NewReadFooterError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read footer").WithCause(cause)
}

func NewWriteFooterError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to write footer").WithCause(cause)
}

func NewResetZstdDecoderError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to reset zstd decoder").WithCause(cause)
}

func NewDataSizeExceedsMaxError(dataSize, maxSize uint64) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("data size %d exceeds maximum %d", dataSize, maxSize))
}

func NewReadDataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read data for validation").WithCause(cause)
}

func NewReadTOCError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read TOC").WithCause(cause)
}

func NewDecompressTOCError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decompress TOC").WithCause(cause)
}

func NewDecodeTOCError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decode TOC").WithCause(cause)
}

func NewReadMetadataFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read metadata frame").WithCause(cause)
}

func NewDecodeMetadataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decode metadata").WithCause(cause)
}

func NewReadEntriesFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read entries frame").WithCause(cause)
}

func NewDecodeEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decode entries").WithCause(cause)
}

func NewInvalidEntryFormatError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid entry format at index %d", index))
}

func NewResourceNotTreeError(id string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("resource %s is not a tree", id))
}

func NewResourceNotBlobError(id string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("resource %s is not a blob", id))
}

func NewResourceNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("resource not found: %s", id))
}

func NewReadResourceFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read resource frame").WithCause(cause)
}

func NewDecodeTreeResourceError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decode tree resource").WithCause(cause)
}

func NewDecodeBlobResourceError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to decode blob resource").WithCause(cause)
}

func NewUnknownResourceTypeError(resourceType string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unknown resource type: %s", resourceType))
}

func NewReadFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read frame").WithCause(cause)
}

func NewReadFrameDataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read frame data").WithCause(cause)
}

func NewGetMetadataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get metadata").WithCause(cause)
}

func NewGetEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get entries").WithCause(cause)
}

func NewNormalizeEntryError(index int, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to normalize entry %d", index)).WithCause(cause)
}

func NewCreateMetadataFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create metadata frame").WithCause(cause)
}

func NewCreateEntriesFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create entries frame").WithCause(cause)
}

func NewProcessFilesystemError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to process filesystem").WithCause(cause)
}

func NewCreateResourceFrameError(resourceID string, cause error) apierror.Error {
	msg := "failed to create resource frame"
	if resourceID != "" {
		msg = fmt.Sprintf("failed to create resource frame for %s", resourceID)
	}
	return apierror.New(apierror.Internal, msg).WithCause(cause)
}

func NewProcessResourceFilesystemError(resourceID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to process filesystem %s", resourceID)).WithCause(cause)
}

func NewStatFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to stat %s", filePath)).WithCause(cause)
}

func NewOpenFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to open %s", filePath)).WithCause(cause)
}

func NewReadFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to read %s", filePath)).WithCause(cause)
}

func NewCreateZstdWriterError(filePath string, cause error) apierror.Error {
	msg := "failed to create zstd writer"
	if filePath != "" {
		msg = fmt.Sprintf("failed to create zstd writer for %s", filePath)
	}
	return apierror.New(apierror.Internal, msg).WithCause(cause)
}

func NewCompressFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to compress %s", filePath)).WithCause(cause)
}

func NewCloseZstdWriterError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to close zstd writer for %s", filePath)).WithCause(cause)
}

func NewMsgpackEncodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to msgpack encode").WithCause(cause)
}

func NewZstdCompressError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to zstd compress").WithCause(cause)
}

func NewInsufficientFramesError(got, need int) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("insufficient frames: got %d, need at least %d", got, need))
}

func NewWriteFrameError(frameIndex int, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to write frame %d", frameIndex)).WithCause(cause)
}

func NewCreateTOCFrameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create TOC frame").WithCause(cause)
}

func NewWriteTOCError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to write TOC").WithCause(cause)
}

func NewTranscodePayloadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode payload").WithCause(cause)
}

func NewInvalidResourceCountError(count int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid resource count: %d exceeds safe conversion limit", count))
}
