// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"reflect"
	"syscall"
	"time"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	TypesNamespace = "wasi:filesystem/types@0.2.8"
)

// TypesHost implements wasi:filesystem/types using fsapi.FS abstractions.
type TypesHost struct {
	resources *preview2.ResourceTable
}

func NewTypesHost(resources *preview2.ResourceTable) *TypesHost {
	return &TypesHost{resources: resources}
}

func (h *TypesHost) Namespace() string {
	return TypesNamespace
}

// Error represents a WASI filesystem error.
type Error struct {
	Code ErrorCode
}

type ErrorCode uint8

const (
	ErrorAccess ErrorCode = iota
	ErrorWouldBlock
	ErrorAlready
	ErrorBadDescriptor
	ErrorBusy
	ErrorDeadlock
	ErrorQuota
	ErrorExist
	ErrorFileTooLarge
	ErrorIllegalByteSequence
	ErrorInProgress
	ErrorInterrupted
	ErrorInvalid
	ErrorIo
	ErrorIsDirectory
	ErrorLoop
	ErrorTooManyLinks
	ErrorMessageSize
	ErrorNameTooLong
	ErrorNoDevice
	ErrorNoEntry
	ErrorNoLock
	ErrorInsufficientMemory
	ErrorInsufficientSpace
	ErrorNotDirectory
	ErrorNotEmpty
	ErrorNotRecoverable
	ErrorUnsupported
	ErrorNoTty
	ErrorNoSuchDevice
	ErrorOverflow
	ErrorNotPermitted
	ErrorPipe
	ErrorReadOnly
	ErrorInvalidSeek
	ErrorTextFileBusy
	ErrorCrossDevice
)

func (e *Error) Error() string {
	return "filesystem error"
}

func mapOSError(err error) *Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, preview2.ErrHostBufferLimit) {
		return &Error{Code: ErrorInsufficientMemory}
	}
	if errors.Is(err, errors.ErrUnsupported) {
		return &Error{Code: ErrorUnsupported}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return &Error{Code: ErrorNoEntry}
	}
	if errors.Is(err, fs.ErrPermission) {
		return &Error{Code: ErrorAccess}
	}
	if errors.Is(err, fs.ErrExist) {
		return &Error{Code: ErrorExist}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return mapErrno(errno)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) {
			return mapErrno(errno)
		}
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return mapErrno(errno)
		}
	}
	return &Error{Code: ErrorIo}
}

func mapErrno(errno syscall.Errno) *Error {
	switch errno {
	case syscall.EACCES, syscall.EPERM:
		return &Error{Code: ErrorAccess}
	case syscall.ENOENT:
		return &Error{Code: ErrorNoEntry}
	case syscall.EEXIST:
		return &Error{Code: ErrorExist}
	case syscall.ENOTDIR:
		return &Error{Code: ErrorNotDirectory}
	case syscall.EISDIR:
		return &Error{Code: ErrorIsDirectory}
	case syscall.ENOTEMPTY:
		return &Error{Code: ErrorNotEmpty}
	case syscall.ENAMETOOLONG:
		return &Error{Code: ErrorNameTooLong}
	case syscall.ENOSPC:
		return &Error{Code: ErrorInsufficientSpace}
	case syscall.EROFS:
		return &Error{Code: ErrorReadOnly}
	case syscall.EXDEV:
		return &Error{Code: ErrorCrossDevice}
	case syscall.ELOOP:
		return &Error{Code: ErrorLoop}
	case syscall.EMLINK:
		return &Error{Code: ErrorTooManyLinks}
	case syscall.EBUSY:
		return &Error{Code: ErrorBusy}
	case syscall.EINVAL:
		return &Error{Code: ErrorInvalid}
	default:
		return &Error{Code: ErrorIo}
	}
}

type DescriptorType uint8

const (
	DescriptorTypeUnknown DescriptorType = iota
	DescriptorTypeBlockDevice
	DescriptorTypeCharacterDevice
	DescriptorTypeDirectory
	DescriptorTypeFifo
	DescriptorTypeSymbolicLink
	DescriptorTypeRegularFile
	DescriptorTypeSocket
)

type Datetime struct {
	Seconds     uint64
	Nanoseconds uint32
}

// DescriptorStat must mirror every field of the wasi:filesystem descriptor-stat
// record. The Canonical ABI encoder maps WIT record fields to Go struct fields by
// name; a missing field (previously link-count and the timestamps) fails the record
// lowering so later fields like size never reach the guest, which then reads garbage.
type DescriptorStat struct {
	DataAccessTimestamp       *Datetime
	DataModificationTimestamp *Datetime
	StatusChangeTimestamp     *Datetime
	LinkCount                 uint64
	Size                      uint64
	Type                      DescriptorType
}

func toDatetime(t time.Time) *Datetime {
	return &Datetime{Seconds: uint64(t.Unix()), Nanoseconds: uint32(t.Nanosecond())}
}

func (h *TypesHost) getDescriptor(handle uint32) (*descriptorResource, *Error) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}
	desc, ok := r.(*descriptorResource)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}
	return desc, nil
}

func (d *descriptorResource) requireFile() (*retainedFile, *Error) {
	if d.file == nil {
		return nil, &Error{Code: ErrorIo}
	}
	return d.file, nil
}

func (d *descriptorResource) borrowDirectory() (fs.File, func(), *Error) {
	if !d.isDir {
		return nil, nil, &Error{Code: ErrorNotDirectory}
	}
	if d.file == nil {
		return nil, nil, &Error{Code: ErrorBadDescriptor}
	}
	dir, release, err := d.file.borrow()
	if err != nil {
		return nil, nil, mapOSError(err)
	}
	return dir, release, nil
}

func sameFilesystem(left, right fsapi.FS) bool {
	if left == nil || right == nil || reflect.TypeOf(left) != reflect.TypeOf(right) {
		return false
	}
	typ := reflect.TypeOf(left)
	return typ.Comparable() && left == right
}

func fileInfoToDescriptorType(info fs.FileInfo) DescriptorType {
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return DescriptorTypeDirectory
	case mode.IsRegular():
		return DescriptorTypeRegularFile
	case mode&os.ModeSymlink != 0:
		return DescriptorTypeSymbolicLink
	case mode&os.ModeNamedPipe != 0:
		return DescriptorTypeFifo
	case mode&os.ModeSocket != 0:
		return DescriptorTypeSocket
	case mode&os.ModeDevice != 0:
		if mode&os.ModeCharDevice != 0 {
			return DescriptorTypeCharacterDevice
		}
		return DescriptorTypeBlockDevice
	default:
		return DescriptorTypeUnknown
	}
}

func (h *TypesHost) FilesystemErrorCode(_ context.Context, err *Error) ErrorCode {
	if err == nil {
		return ErrorIo
	}
	return err.Code
}

func boundedAllocationSize(length uint64) (uint64, bool) {
	if length > preview2.MaxAllocationSize {
		return 0, false
	}
	return length, true
}

// MethodDescriptorRead implements wasi:filesystem/types descriptor.read, whose
// WIT result is result<tuple<list<u8>, bool>, error-code>: the bytes read plus an
// end-of-stream flag. The tuple is returned as []any{data, eof}.
func (h *TypesHost) MethodDescriptorRead(_ context.Context, self uint32, length uint64, offset uint64) ([]any, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	if desc.isDir {
		return nil, &Error{Code: ErrorIsDirectory}
	}
	if !desc.readable {
		return nil, &Error{Code: ErrorNotPermitted}
	}
	if offset > math.MaxInt64 {
		return nil, &Error{Code: ErrorOverflow}
	}

	// Descriptor reads return a transient list, so cap them to one stream-sized
	// chunk. The guest can issue another offset read; allocating up to 1GiB here
	// would bypass the explicit host-buffer admission used by streams.
	if length > preview2.DefaultBufferSize {
		length = preview2.DefaultBufferSize
	}
	allocationSize, ok := boundedAllocationSize(length)
	if !ok {
		return nil, &Error{Code: ErrorInsufficientMemory}
	}

	buf := make([]byte, allocationSize)
	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return nil, fsErr
	}
	n, readErr := file.readAt(buf, int64(offset))
	eof := false
	if readErr != nil {
		if errors.Is(readErr, io.EOF) {
			eof = true
		} else if n == 0 {
			return nil, mapOSError(readErr)
		}
	}

	return []any{buf[:n], eof}, nil
}

func (h *TypesHost) MethodDescriptorWrite(_ context.Context, self uint32, buffer []byte, offset uint64) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if !desc.writable {
		return 0, &Error{Code: ErrorReadOnly}
	}

	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}
	if offset > math.MaxInt64 {
		return 0, &Error{Code: ErrorOverflow}
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return 0, fsErr
	}
	n, writeErr := file.writeAt(buffer, int64(offset))
	if writeErr != nil {
		return uint64(n), mapOSError(writeErr)
	}

	return uint64(n), nil
}

func (h *TypesHost) MethodDescriptorGetType(_ context.Context, self uint32) (DescriptorType, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return DescriptorTypeUnknown, err
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return DescriptorTypeUnknown, fsErr
	}
	info, statErr := file.stat()
	if statErr != nil {
		return DescriptorTypeUnknown, mapOSError(statErr)
	}

	return fileInfoToDescriptorType(info), nil
}

func (h *TypesHost) MethodDescriptorStat(_ context.Context, self uint32) (*DescriptorStat, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return nil, fsErr
	}
	info, statErr := file.stat()
	if statErr != nil {
		return nil, mapOSError(statErr)
	}

	mod := toDatetime(info.ModTime())
	return &DescriptorStat{
		Type:                      fileInfoToDescriptorType(info),
		LinkCount:                 1,
		Size:                      uint64(info.Size()),
		DataAccessTimestamp:       mod,
		DataModificationTimestamp: mod,
		StatusChangeTimestamp:     mod,
	}, nil
}

func (h *TypesHost) MethodDescriptorSeek(_ context.Context, self uint32, offset int64, whence uint8) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	var newPosition int64
	switch whence {
	case 0:
		newPosition = offset
	case 1:
		newPosition = desc.position + offset
	case 2:
		file, fsErr := desc.requireFile()
		if fsErr != nil {
			return 0, fsErr
		}
		info, statErr := file.stat()
		if statErr != nil {
			return 0, mapOSError(statErr)
		}
		newPosition = info.Size() + offset
	default:
		return 0, &Error{Code: ErrorInvalid}
	}

	if newPosition < 0 {
		return 0, &Error{Code: ErrorInvalid}
	}

	desc.position = newPosition
	return uint64(newPosition), nil
}

func (h *TypesHost) MethodDescriptorGetFlags(_ context.Context, self uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	var flags uint32
	if desc.readable {
		flags |= 1
	}
	if desc.writable {
		flags |= 2
	}
	if desc.mutable {
		flags |= 32
	}
	return flags, nil
}

func (h *TypesHost) MethodDescriptorOpenAt(_ context.Context, self uint32, pathFlags uint32, path string, openFlags uint32, descriptorFlags uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	if !desc.isDir {
		return 0, &Error{Code: ErrorNotDirectory}
	}
	if pathFlags&^uint32(1) != 0 || openFlags&^uint32(0x0f) != 0 || descriptorFlags&^uint32(0x3f) != 0 {
		return 0, &Error{Code: ErrorInvalid}
	}
	if descriptorFlags&0x1c != 0 {
		return 0, &Error{Code: ErrorUnsupported}
	}
	if openFlags&4 != 0 && openFlags&1 == 0 {
		return 0, &Error{Code: ErrorInvalid}
	}
	request := fsapi.DescriptorOpenRequest{
		Read:            descriptorFlags&1 != 0,
		Write:           descriptorFlags&2 != 0,
		MutateDirectory: descriptorFlags&32 != 0,
		Create:          openFlags&1 != 0,
		Directory:       openFlags&2 != 0,
		Exclusive:       openFlags&4 != 0,
		Truncate:        openFlags&8 != 0,
		NoFollow:        pathFlags&1 == 0,
	}
	if request.Write && !desc.writable && !desc.mutable {
		return 0, &Error{Code: ErrorReadOnly}
	}
	if request.MutateDirectory && !desc.mutable {
		return 0, &Error{Code: ErrorReadOnly}
	}
	if request.Read && !desc.readable {
		return 0, &Error{Code: ErrorNotPermitted}
	}
	if (request.Create || request.Exclusive) && !desc.mutable {
		return 0, &Error{Code: ErrorReadOnly}
	}
	if request.Truncate && !desc.writable && !desc.mutable {
		return 0, &Error{Code: ErrorReadOnly}
	}
	opener, ok := desc.fs.(fsapi.DescriptorOpener)
	if !ok {
		return 0, &Error{Code: ErrorUnsupported}
	}
	dir, release, borrowErr := desc.borrowDirectory()
	if borrowErr != nil {
		return 0, borrowErr
	}
	defer release()
	file, openErr := opener.OpenDescriptorAt(dir, path, request)
	if openErr != nil {
		return 0, mapOSError(openErr)
	}
	newDesc, newErr := newOpenedDescriptorResource(desc.fs, file, request.Read, request.Write && (desc.writable || desc.mutable), request.MutateDirectory && desc.mutable)
	if newErr != nil {
		return 0, mapOSError(newErr)
	}
	handle := h.resources.Add(newDesc)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorCreateDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}
	if !desc.mutable {
		return &Error{Code: ErrorReadOnly}
	}
	mutator, ok := desc.fs.(fsapi.DescriptorMutator)
	if !ok {
		return &Error{Code: ErrorUnsupported}
	}
	dir, release, dirErr := desc.borrowDirectory()
	if dirErr != nil {
		return dirErr
	}
	defer release()
	if createErr := mutator.CreateDirectoryAt(dir, path, 0755); createErr != nil {
		return mapOSError(createErr)
	}
	return nil
}

func (h *TypesHost) MethodDescriptorReadDirectory(_ context.Context, self uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if !desc.isDir {
		return 0, &Error{Code: ErrorNotDirectory}
	}
	if !desc.readable {
		return 0, &Error{Code: ErrorNotPermitted}
	}

	stream, streamErr := newDirectoryEntryStreamResource(desc)
	if streamErr != nil {
		return 0, mapOSError(streamErr)
	}
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorSync(_ context.Context, self uint32) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}
	if desc.isDir || desc.readOnly {
		return nil
	}
	file, fileErr := desc.requireFile()
	if fileErr != nil {
		return fileErr
	}
	if syncErr := file.sync(); syncErr != nil {
		return mapOSError(syncErr)
	}
	return nil
}

func (h *TypesHost) MethodDescriptorSyncData(ctx context.Context, self uint32) *Error {
	return h.MethodDescriptorSync(ctx, self)
}

func (h *TypesHost) MethodDescriptorReadViaStream(_ context.Context, self uint32, offset uint64) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}
	if !desc.readable {
		return 0, &Error{Code: ErrorNotPermitted}
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return 0, fsErr
	}
	stream, streamErr := newFileInputStreamResource(file, offset, h.resources.HostBufferBudget())
	if streamErr != nil {
		return 0, mapOSError(streamErr)
	}
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorWriteViaStream(_ context.Context, self uint32, offset uint64) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}
	if !desc.writable {
		return 0, &Error{Code: ErrorReadOnly}
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return 0, fsErr
	}
	stream, streamErr := newFileOutputStreamResource(file, offset, false, h.resources.HostBufferBudget())
	if streamErr != nil {
		return 0, mapOSError(streamErr)
	}
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorAppendViaStream(_ context.Context, self uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}
	if !desc.writable {
		return 0, &Error{Code: ErrorReadOnly}
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return 0, fsErr
	}
	stream, streamErr := newFileOutputStreamResource(file, 0, true, h.resources.HostBufferBudget())
	if streamErr != nil {
		return 0, mapOSError(streamErr)
	}
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorMetadataHash(_ context.Context, self uint32) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	file, fsErr := desc.requireFile()
	if fsErr != nil {
		return 0, fsErr
	}
	info, statErr := file.stat()
	if statErr != nil {
		return 0, mapOSError(statErr)
	}

	hash := uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	return hash, nil
}

func (h *TypesHost) MethodDescriptorMetadataHashAt(_ context.Context, self uint32, pathFlags uint32, path string) (uint64, *Error) {
	stat, err := h.MethodDescriptorStatAt(context.Background(), self, pathFlags, path)
	if err != nil {
		return 0, err
	}
	hash := stat.Size ^ (stat.DataModificationTimestamp.Seconds*1_000_000_000 + uint64(stat.DataModificationTimestamp.Nanoseconds))
	return hash, nil
}

func (h *TypesHost) MethodDescriptorRenameAt(_ context.Context, self uint32, oldPath string, newDescriptor uint32, newPath string) *Error {
	oldDesc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !oldDesc.mutable {
		return &Error{Code: ErrorReadOnly}
	}

	newDesc, err := h.getDescriptor(newDescriptor)
	if err != nil {
		return err
	}

	if !newDesc.mutable {
		return &Error{Code: ErrorReadOnly}
	}

	if !sameFilesystem(oldDesc.fs, newDesc.fs) {
		return &Error{Code: ErrorCrossDevice}
	}
	mutator, ok := oldDesc.fs.(fsapi.DescriptorMutator)
	if !ok {
		return &Error{Code: ErrorUnsupported}
	}
	oldDir, releaseOld, oldDirErr := oldDesc.borrowDirectory()
	if oldDirErr != nil {
		return oldDirErr
	}
	defer releaseOld()
	newDir, releaseNew, newDirErr := newDesc.borrowDirectory()
	if newDirErr != nil {
		return newDirErr
	}
	defer releaseNew()
	if renameErr := mutator.RenameAt(oldDir, oldPath, newDir, newPath); renameErr != nil {
		return mapOSError(renameErr)
	}
	return nil
}

func (h *TypesHost) MethodDescriptorUnlinkFileAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !desc.mutable {
		return &Error{Code: ErrorReadOnly}
	}

	mutator, ok := desc.fs.(fsapi.DescriptorMutator)
	if !ok {
		return &Error{Code: ErrorUnsupported}
	}
	dir, release, dirErr := desc.borrowDirectory()
	if dirErr != nil {
		return dirErr
	}
	defer release()
	if removeErr := mutator.UnlinkFileAt(dir, path); removeErr != nil {
		return mapOSError(removeErr)
	}
	return nil
}

func (h *TypesHost) MethodDescriptorRemoveDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !desc.mutable {
		return &Error{Code: ErrorReadOnly}
	}

	mutator, ok := desc.fs.(fsapi.DescriptorMutator)
	if !ok {
		return &Error{Code: ErrorUnsupported}
	}
	dir, release, dirErr := desc.borrowDirectory()
	if dirErr != nil {
		return dirErr
	}
	defer release()
	if removeErr := mutator.RemoveDirectoryAt(dir, path); removeErr != nil {
		return mapOSError(removeErr)
	}
	return nil
}

func (h *TypesHost) MethodDescriptorStatAt(_ context.Context, self uint32, pathFlags uint32, path string) (*DescriptorStat, *Error) {
	opened, err := h.MethodDescriptorOpenAt(context.Background(), self, pathFlags, path, 0, 1)
	if err != nil {
		return nil, err
	}
	defer h.resources.Remove(opened)
	return h.MethodDescriptorStat(context.Background(), opened)
}

func (h *TypesHost) MethodDescriptorSymlinkAt(_ context.Context, _ uint32, _ string, _ string) *Error {
	return &Error{Code: ErrorUnsupported}
}

func (h *TypesHost) MethodDescriptorReadlinkAt(_ context.Context, _ uint32, _ string) (string, *Error) {
	return "", &Error{Code: ErrorUnsupported}
}

func (h *TypesHost) MethodDescriptorLinkAt(_ context.Context, _ uint32, _ uint32, _ string, _ uint32, _ string) *Error {
	return &Error{Code: ErrorUnsupported}
}

func (h *TypesHost) MethodDescriptorSetTimes(_ context.Context, self uint32, _ uint64, _ uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !desc.writable {
		return &Error{Code: ErrorReadOnly}
	}

	return &Error{Code: ErrorUnsupported}
}

func (h *TypesHost) MethodDescriptorSetTimesAt(_ context.Context, self uint32, _ uint32, _ string, _ uint64, _ uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !desc.writable {
		return &Error{Code: ErrorReadOnly}
	}

	return &Error{Code: ErrorUnsupported}
}

func (h *TypesHost) MethodDescriptorSetSize(_ context.Context, self uint32, size uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if !desc.writable {
		return &Error{Code: ErrorReadOnly}
	}

	if desc.isDir {
		return &Error{Code: ErrorIsDirectory}
	}
	if size > math.MaxInt64 {
		return &Error{Code: ErrorOverflow}
	}

	file, fileErr := desc.requireFile()
	if fileErr != nil {
		return fileErr
	}
	if truncateErr := file.truncate(int64(size)); truncateErr != nil {
		return mapOSError(truncateErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorAdvise(_ context.Context, _ uint32, _ uint64, _ uint64, _ uint8) *Error {
	return nil
}

func (h *TypesHost) MethodDescriptorIsSameObject(_ context.Context, self uint32, other uint32) bool {
	selfDesc, err := h.getDescriptor(self)
	if err != nil {
		return false
	}

	otherDesc, err := h.getDescriptor(other)
	if err != nil {
		return false
	}

	if selfDesc.file == nil || otherDesc.file == nil {
		return false
	}
	if selfDesc.file == otherDesc.file {
		return true
	}
	selfInfo, selfErr := selfDesc.file.stat()
	otherInfo, otherErr := otherDesc.file.stat()
	return selfErr == nil && otherErr == nil && os.SameFile(selfInfo, otherInfo)
}

func (h *TypesHost) ResourceDropDescriptor(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *TypesHost) ResourceDropDirectoryEntryStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *TypesHost) MethodDirectoryEntryStreamReadDirectoryEntry(_ context.Context, self uint32) (*preview2.DirectoryEntry, *Error) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}

	if stream, ok := r.(*directoryEntryStreamResource); ok {
		entry, streamErr := stream.readNext()
		if streamErr != nil {
			return nil, mapOSError(streamErr)
		}
		if entry == nil {
			return nil, nil
		}
		info, _ := entry.Info()
		dtype := uint8(DescriptorTypeRegularFile)
		if info != nil {
			dtype = uint8(fileInfoToDescriptorType(info))
		} else if entry.IsDir() {
			dtype = uint8(DescriptorTypeDirectory)
		}
		return &preview2.DirectoryEntry{Type: dtype, Name: entry.Name()}, nil
	}
	// Keep decoding existing backend directory stream handles, including any
	// supplied by a sibling host implementation during migration.
	stream, ok := r.(*preview2.DirectoryEntryStreamResource)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}
	return stream.ReadNext(), nil
}

func (h *TypesHost) Register() map[string]any {
	return map[string]any{
		"filesystem-error-code":                               h.FilesystemErrorCode,
		"[method]descriptor.read":                             h.MethodDescriptorRead,
		"[method]descriptor.write":                            h.MethodDescriptorWrite,
		"[method]descriptor.get-type":                         h.MethodDescriptorGetType,
		"[method]descriptor.stat":                             h.MethodDescriptorStat,
		"[method]descriptor.stat-at":                          h.MethodDescriptorStatAt,
		"[method]descriptor.seek":                             h.MethodDescriptorSeek,
		"[method]descriptor.get-flags":                        h.MethodDescriptorGetFlags,
		"[method]descriptor.open-at":                          h.MethodDescriptorOpenAt,
		"[method]descriptor.create-directory-at":              h.MethodDescriptorCreateDirectoryAt,
		"[method]descriptor.read-directory":                   h.MethodDescriptorReadDirectory,
		"[method]descriptor.sync":                             h.MethodDescriptorSync,
		"[method]descriptor.sync-data":                        h.MethodDescriptorSyncData,
		"[method]descriptor.read-via-stream":                  h.MethodDescriptorReadViaStream,
		"[method]descriptor.write-via-stream":                 h.MethodDescriptorWriteViaStream,
		"[method]descriptor.append-via-stream":                h.MethodDescriptorAppendViaStream,
		"[method]descriptor.metadata-hash":                    h.MethodDescriptorMetadataHash,
		"[method]descriptor.metadata-hash-at":                 h.MethodDescriptorMetadataHashAt,
		"[method]descriptor.rename-at":                        h.MethodDescriptorRenameAt,
		"[method]descriptor.unlink-file-at":                   h.MethodDescriptorUnlinkFileAt,
		"[method]descriptor.remove-directory-at":              h.MethodDescriptorRemoveDirectoryAt,
		"[method]descriptor.symlink-at":                       h.MethodDescriptorSymlinkAt,
		"[method]descriptor.readlink-at":                      h.MethodDescriptorReadlinkAt,
		"[method]descriptor.link-at":                          h.MethodDescriptorLinkAt,
		"[method]descriptor.set-times":                        h.MethodDescriptorSetTimes,
		"[method]descriptor.set-times-at":                     h.MethodDescriptorSetTimesAt,
		"[method]descriptor.set-size":                         h.MethodDescriptorSetSize,
		"[method]descriptor.advise":                           h.MethodDescriptorAdvise,
		"[method]descriptor.is-same-object":                   h.MethodDescriptorIsSameObject,
		"[method]directory-entry-stream.read-directory-entry": h.MethodDirectoryEntryStreamReadDirectoryEntry,
		"[resource-drop]descriptor":                           h.ResourceDropDescriptor,
		"[resource-drop]directory-entry-stream":               h.ResourceDropDirectoryEntryStream,
	}
}
