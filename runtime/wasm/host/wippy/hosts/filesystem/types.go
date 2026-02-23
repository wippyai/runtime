// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	TypesNamespace = "wasi:filesystem/types@0.2.3"
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
	if errors.Is(err, fs.ErrNotExist) {
		return &Error{Code: ErrorNoEntry}
	}
	if errors.Is(err, fs.ErrPermission) {
		return &Error{Code: ErrorAccess}
	}
	if errors.Is(err, fs.ErrExist) {
		return &Error{Code: ErrorExist}
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

type DescriptorStat struct {
	Type DescriptorType
	Size uint64
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

// resolvePath normalizes a relative path for use with the descriptor's FS.
func resolvePath(desc *descriptorResource, path string) string {
	if desc.path == "." || desc.path == "" {
		return path
	}
	return desc.path + "/" + path
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

func (h *TypesHost) MethodDescriptorRead(_ context.Context, self uint32, length uint64, offset uint64) ([]byte, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	if desc.isDir {
		return nil, &Error{Code: ErrorIsDirectory}
	}

	file, fsErr := desc.fs.OpenFile(desc.path, os.O_RDONLY, 0)
	if fsErr != nil {
		return nil, mapOSError(fsErr)
	}
	defer func() { _ = file.Close() }()

	if offset > 0 {
		_, fsErr = file.Seek(int64(offset), 0)
		if fsErr != nil {
			return nil, mapOSError(fsErr)
		}
	}

	if length > preview2.MaxAllocationSize {
		length = preview2.MaxAllocationSize
	}

	buf := make([]byte, length)
	n, fsErr := file.Read(buf)
	if fsErr != nil && n == 0 {
		return nil, mapOSError(fsErr)
	}

	return buf[:n], nil
}

func (h *TypesHost) MethodDescriptorWrite(_ context.Context, self uint32, buffer []byte, offset uint64) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.readOnly {
		return 0, &Error{Code: ErrorReadOnly}
	}

	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	file, fsErr := desc.fs.OpenFile(desc.path, os.O_WRONLY, 0)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}
	defer func() { _ = file.Close() }()

	_, fsErr = file.Seek(int64(offset), 0)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	n, fsErr := file.Write(buffer)
	if fsErr != nil {
		return uint64(n), mapOSError(fsErr)
	}

	return uint64(n), nil
}

func (h *TypesHost) MethodDescriptorGetType(_ context.Context, self uint32) (DescriptorType, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return DescriptorTypeUnknown, err
	}

	info, fsErr := desc.fs.Lstat(desc.path)
	if fsErr != nil {
		return DescriptorTypeUnknown, mapOSError(fsErr)
	}

	return fileInfoToDescriptorType(info), nil
}

func (h *TypesHost) MethodDescriptorStat(_ context.Context, self uint32) (*DescriptorStat, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	info, fsErr := desc.fs.Stat(desc.path)
	if fsErr != nil {
		return nil, mapOSError(fsErr)
	}

	return &DescriptorStat{
		Type: fileInfoToDescriptorType(info),
		Size: uint64(info.Size()),
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
		info, fsErr := desc.fs.Stat(desc.path)
		if fsErr != nil {
			return 0, mapOSError(fsErr)
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

func (h *TypesHost) MethodDescriptorGetFlags(_ context.Context, _ uint32) (uint32, *Error) {
	return 0, nil
}

func (h *TypesHost) MethodDescriptorOpenAt(_ context.Context, self uint32, _ uint32, path string, openFlags uint32, _ uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	fullPath := resolvePath(desc, path)
	createFlag := (openFlags & 1) != 0

	info, fsErr := desc.fs.Stat(fullPath)
	if fsErr != nil {
		if errors.Is(fsErr, fs.ErrNotExist) && createFlag && !desc.readOnly {
			file, createErr := desc.fs.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if createErr != nil {
				return 0, mapOSError(createErr)
			}
			_ = file.Close()
			info, fsErr = desc.fs.Stat(fullPath)
			if fsErr != nil {
				return 0, mapOSError(fsErr)
			}
		} else {
			return 0, mapOSError(fsErr)
		}
	}

	newDesc := newDescriptorResource(desc.fs, fullPath, info.IsDir(), desc.readOnly)
	handle := h.resources.Add(newDesc)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorCreateDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath := resolvePath(desc, path)
	fsErr := desc.fs.Mkdir(fullPath, 0755)
	if fsErr != nil {
		return mapOSError(fsErr)
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

	entries, fsErr := desc.fs.ReadDir(desc.path)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	dirEntries := make([]preview2.DirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		info, _ := entry.Info()
		var dtype uint8
		if info != nil {
			dtype = uint8(fileInfoToDescriptorType(info))
		} else if entry.IsDir() {
			dtype = uint8(DescriptorTypeDirectory)
		} else {
			dtype = uint8(DescriptorTypeRegularFile)
		}
		dirEntries = append(dirEntries, preview2.DirectoryEntry{
			Type: dtype,
			Name: entry.Name(),
		})
	}

	stream := preview2.NewDirectoryEntryStreamResource(dirEntries)
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorSync(_ context.Context, _ uint32) *Error {
	return nil
}

func (h *TypesHost) MethodDescriptorSyncData(_ context.Context, _ uint32) *Error {
	return nil
}

func (h *TypesHost) MethodDescriptorReadViaStream(_ context.Context, self uint32, offset uint64) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.isDir {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	data, fsErr := readAllFrom(desc.fs, desc.path)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	if offset > uint64(len(data)) {
		data = nil
	} else {
		data = data[offset:]
	}

	stream := preview2.NewInputStreamResource(data)
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
	if desc.readOnly {
		return 0, &Error{Code: ErrorReadOnly}
	}

	flags := os.O_WRONLY | os.O_CREATE
	file, fsErr := desc.fs.OpenFile(desc.path, flags, 0644)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	if offset > 0 {
		_, fsErr = file.Seek(int64(offset), 0)
		if fsErr != nil {
			_ = file.Close()
			return 0, mapOSError(fsErr)
		}
	}

	stream := newFileOutputStreamResource(file)
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
	if desc.readOnly {
		return 0, &Error{Code: ErrorReadOnly}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	file, fsErr := desc.fs.OpenFile(desc.path, flags, 0644)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	stream := newFileOutputStreamResource(file)
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorMetadataHash(_ context.Context, self uint32) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	info, fsErr := desc.fs.Stat(desc.path)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	hash := uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	return hash, nil
}

func (h *TypesHost) MethodDescriptorMetadataHashAt(_ context.Context, self uint32, _ uint32, path string) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	fullPath := resolvePath(desc, path)
	info, fsErr := desc.fs.Stat(fullPath)
	if fsErr != nil {
		return 0, mapOSError(fsErr)
	}

	hash := uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	return hash, nil
}

func (h *TypesHost) MethodDescriptorRenameAt(_ context.Context, self uint32, oldPath string, newDescriptor uint32, newPath string) *Error {
	oldDesc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if oldDesc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	newDesc, err := h.getDescriptor(newDescriptor)
	if err != nil {
		return err
	}

	if newDesc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	oldFullPath := resolvePath(oldDesc, oldPath)
	newFullPath := resolvePath(newDesc, newPath)

	fsErr := oldDesc.fs.Rename(oldFullPath, newFullPath)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorUnlinkFileAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath := resolvePath(desc, path)
	info, fsErr := desc.fs.Lstat(fullPath)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	if info.IsDir() {
		return &Error{Code: ErrorIsDirectory}
	}

	fsErr = desc.fs.Remove(fullPath)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorRemoveDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath := resolvePath(desc, path)
	info, fsErr := desc.fs.Lstat(fullPath)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	if !info.IsDir() {
		return &Error{Code: ErrorNotDirectory}
	}

	fsErr = desc.fs.Remove(fullPath)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorStatAt(_ context.Context, self uint32, pathFlags uint32, path string) (*DescriptorStat, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	fullPath := resolvePath(desc, path)

	var info fs.FileInfo
	var fsErr error
	if pathFlags&1 != 0 {
		info, fsErr = desc.fs.Stat(fullPath)
	} else {
		info, fsErr = desc.fs.Lstat(fullPath)
	}
	if fsErr != nil {
		return nil, mapOSError(fsErr)
	}

	return &DescriptorStat{
		Type: fileInfoToDescriptorType(info),
		Size: uint64(info.Size()),
	}, nil
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

func (h *TypesHost) MethodDescriptorSetTimes(_ context.Context, self uint32, atimeNs uint64, mtimeNs uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	atime := time.Unix(0, int64(atimeNs))
	mtime := time.Unix(0, int64(mtimeNs))

	fsErr := desc.fs.Chtimes(desc.path, atime, mtime)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorSetTimesAt(_ context.Context, self uint32, _ uint32, path string, atimeNs uint64, mtimeNs uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath := resolvePath(desc, path)
	atime := time.Unix(0, int64(atimeNs))
	mtime := time.Unix(0, int64(mtimeNs))

	fsErr := desc.fs.Chtimes(fullPath, atime, mtime)
	if fsErr != nil {
		return mapOSError(fsErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorSetSize(_ context.Context, self uint32, size uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.readOnly {
		return &Error{Code: ErrorReadOnly}
	}

	if desc.isDir {
		return &Error{Code: ErrorIsDirectory}
	}

	fsErr := desc.fs.Truncate(desc.path, int64(size))
	if fsErr != nil {
		return mapOSError(fsErr)
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

	return selfDesc.fs == otherDesc.fs && selfDesc.path == otherDesc.path
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

	stream, ok := r.(*preview2.DirectoryEntryStreamResource)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}

	entry := stream.ReadNext()
	if entry == nil {
		return nil, nil
	}

	return entry, nil
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
