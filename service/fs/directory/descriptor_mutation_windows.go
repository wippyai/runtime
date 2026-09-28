//go:build windows

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"unsafe"

	fsapi "github.com/wippyai/runtime/api/fs"
	"golang.org/x/sys/windows"
)

var _ fsapi.DescriptorMutator = (*FS)(nil)

const windowsFileDeleteChild = 0x00000040

func (d *FS) CreateDirectoryAt(directory fs.File, name string, mode fs.FileMode) error {
	if err := d.checkPermissions("mkdir-at", name, permWrite|permExec); err != nil {
		return err
	}
	parent, base, closeParent, err := descriptorWindowsParent(directory, name, windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windows.FILE_APPEND_DATA)
	if err != nil {
		return err
	}
	defer closeParent()
	handle, err := descriptorWindowsCreateRelative(parent, base, windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES, uint32(mode&d.mode&fs.ModePerm), windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE)
	if err == nil {
		_ = windows.CloseHandle(handle)
	}
	return err
}

func (d *FS) RenameAt(oldDirectory fs.File, oldName string, newDirectory fs.File, newName string) error {
	if err := d.checkPermissions("rename-at", oldName, permWrite|permExec); err != nil {
		return err
	}
	sourceParent, sourceBase, closeSourceParent, err := descriptorWindowsParent(oldDirectory, oldName, windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE)
	if err != nil {
		return err
	}
	defer closeSourceParent()
	source, err := descriptorWindowsCreateRelative(sourceParent, sourceBase, windows.SYNCHRONIZE|windows.DELETE|windows.FILE_READ_ATTRIBUTES, 0, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(source)

	var sourceInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(source, &sourceInfo); err != nil {
		return normalizeWindowsNTError(err)
	}
	targetAccess := uint32(windows.SYNCHRONIZE | windows.FILE_READ_ATTRIBUTES | windows.FILE_TRAVERSE)
	if sourceInfo.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		targetAccess |= windows.FILE_APPEND_DATA // FILE_ADD_SUBDIRECTORY
	} else {
		targetAccess |= windows.FILE_WRITE_DATA // FILE_ADD_FILE
	}
	targetParent, targetBase, closeTargetParent, err := descriptorWindowsParent(newDirectory, newName, targetAccess)
	if err != nil {
		return err
	}
	defer closeTargetParent()

	utf16, err := windows.UTF16FromString(targetBase)
	if err != nil {
		return normalizeWindowsNTError(err)
	}
	nameBytes := (len(utf16) - 1) * 2
	bufferSize := int(unsafe.Offsetof(windowsRenameInformation{}.FileName)) + nameBytes
	buffer := make([]byte, bufferSize)
	info := (*windowsRenameInformation)(unsafe.Pointer(&buffer[0]))
	// FileRenameInformation is the legacy structure: ReplaceIfExists is a
	// BOOLEAN, not the flags field used by FileRenameInformationEx.
	info.ReplaceIfExists = 1
	info.RootDirectory = targetParent
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&info.FileName[0])), len(utf16)-1), utf16[:len(utf16)-1])
	var status windows.IO_STATUS_BLOCK
	return normalizeWindowsNTError(windows.NtSetInformationFile(source, &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation))
}

func (d *FS) UnlinkFileAt(directory fs.File, name string) error {
	if err := d.checkPermissions("unlink-at", name, permWrite|permExec); err != nil {
		return err
	}
	return descriptorWindowsDelete(directory, name, windows.FILE_NON_DIRECTORY_FILE)
}

func (d *FS) RemoveDirectoryAt(directory fs.File, name string) error {
	if err := d.checkPermissions("remove-directory-at", name, permWrite|permExec); err != nil {
		return err
	}
	return descriptorWindowsDelete(directory, name, windows.FILE_DIRECTORY_FILE)
}

// FILE_RENAME_INFORMATION has a BOOLEAN followed by natural alignment before
// RootDirectory. Offsetof keeps the variable-sized name buffer correct on both
// 32-bit and 64-bit Windows.
type windowsRenameInformation struct {
	ReplaceIfExists uint8
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func descriptorWindowsDelete(directory fs.File, name string, options uint32) error {
	parent, base, closeParent, err := descriptorWindowsParent(directory, name, windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windowsFileDeleteChild)
	if err != nil {
		return err
	}
	defer closeParent()
	handle, err := descriptorWindowsCreateRelative(parent, base, windows.SYNCHRONIZE|windows.DELETE|windows.FILE_READ_ATTRIBUTES, 0, windows.FILE_OPEN, options|windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var status windows.IO_STATUS_BLOCK
	deleteBuffer := []byte{1}
	return normalizeWindowsNTError(windows.NtSetInformationFile(handle, &status, &deleteBuffer[0], uint32(len(deleteBuffer)), windows.FileDispositionInformation))
}

// descriptorWindowsParent reopens the destination parent with only the rights
// needed for one namespace operation. It resolves the complete parent under
// the retained RootDirectory with OBJ_DONT_REPARSE, then RenameAt passes only
// the basename to NtSetInformationFile. A junction cannot redirect either
// lookup outside the descriptor grant.
func descriptorWindowsParent(directory fs.File, name string, desiredAccess uint32) (windows.Handle, string, func(), error) {
	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return 0, "", func() {}, errors.ErrUnsupported
	}
	path, err := descriptorWindowsPath(name)
	if err != nil || path == "." {
		if err == nil {
			err = fs.ErrInvalid
		}
		return 0, "", func() {}, err
	}
	parts := strings.Split(path, `\`)
	base := parts[len(parts)-1]
	parentPath := "."
	if len(parts) > 1 {
		parentPath = strings.Join(parts[:len(parts)-1], `\`)
	}
	parent, err := descriptorWindowsCreateRelative(windows.Handle(fdSource.Fd()), parentPath, desiredAccess, 0, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return 0, "", func() {}, err
	}
	return parent, base, func() { _ = windows.CloseHandle(parent) }, nil
}

// descriptorWindowsCreateRelative opens a validated relative path below an
// already-open directory HANDLE. mode is retained for the DescriptorMutator
// contract; Windows creation attributes do not encode Unix permission bits.
func descriptorWindowsCreateRelative(root windows.Handle, path string, desiredAccess, mode, disposition, options uint32) (windows.Handle, error) {
	_ = mode
	objectName, err := descriptorWindowsUnicodePath(path)
	if err != nil {
		return 0, normalizeWindowsNTError(err)
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: root,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, desiredAccess, oa, &status, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_FOR_BACKUP_INTENT, 0, 0)
	if err != nil {
		return 0, normalizeWindowsNTError(err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, normalizeWindowsNTError(err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return 0, syscall.ELOOP
	}
	return handle, nil
}
