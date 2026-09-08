//go:build windows

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"unsafe"

	fsapi "github.com/wippyai/runtime/api/fs"
	"golang.org/x/sys/windows"
)

var _ fsapi.DescriptorOpener = (*FS)(nil)

// OpenDescriptorAt uses the retained directory HANDLE as RootDirectory. The
// no-reparse flag makes this a capability operation rather than a path lookup
// from the process working directory. Windows has no openat2 equivalent that
// also proves an in-tree link target for a caller asking to follow links; this
// conservative implementation opens ordinary files safely and rejects every
// reparse point instead of following one outside the descriptor grant.
func (d *FS) OpenDescriptorAt(directory fs.File, name string, request fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	if err := d.checkDescriptorOpenPermissions(name, request); err != nil {
		return nil, err
	}
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: fs.ErrInvalid}
	}
	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	path, pathErr := descriptorWindowsPath(name)
	if pathErr != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: pathErr}
	}
	objectName, err := descriptorWindowsUnicodePath(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(fdSource.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE)
	if request.Read {
		access |= windows.FILE_GENERIC_READ
	}
	if request.Write {
		access |= windows.FILE_GENERIC_WRITE
	}
	disposition, options, dispositionErr := descriptorWindowsOpenOptions(request)
	if dispositionErr != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: dispositionErr}
	}
	var status windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access, oa, &status, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, options, 0, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: normalizeWindowsNTError(err)}
	}
	var info windows.ByHandleFileInformation
	if infoErr := windows.GetFileInformationByHandle(handle, &info); infoErr != nil {
		_ = windows.CloseHandle(handle)
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: normalizeWindowsNTError(infoErr)}
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: syscall.ELOOP}
	}
	file := os.NewFile(uintptr(handle), name)
	fileInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: statErr}
	}
	if !fileInfo.Mode().IsRegular() && !fileInfo.IsDir() {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	return file, nil
}

func normalizeWindowsNTError(err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		if status == windows.STATUS_REPARSE_POINT_ENCOUNTERED {
			return syscall.ELOOP
		}
		errno := status.Errno()
		switch errno {
		case syscall.ERROR_FILE_EXISTS, syscall.ERROR_ALREADY_EXISTS:
			return fs.ErrExist
		case syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_PATH_NOT_FOUND:
			return fs.ErrNotExist
		case windows.ERROR_DIRECTORY:
			// FILE_DIRECTORY_FILE against a regular file is reported as
			// ERROR_DIRECTORY. Normalize it to the portable error that the
			// Preview2 layer maps to not-directory.
			return syscall.ENOTDIR
		case syscall.ERROR_ACCESS_DENIED, syscall.ERROR_PRIVILEGE_NOT_HELD:
			return fs.ErrPermission
		default:
			return errno
		}
	}
	return err
}

func (d *FS) checkDescriptorOpenPermissions(name string, request fsapi.DescriptorOpenRequest) error {
	if request.Write || request.Create || request.Truncate || request.Exclusive {
		if err := d.checkPermissions("open-at", name, permWrite); err != nil {
			return err
		}
	}
	if request.Read {
		if err := d.checkPermissions("open-at", name, permRead); err != nil {
			return err
		}
	}
	return d.checkPermissions("open-at", name, permExec)
}

func descriptorWindowsPath(name string) (string, error) {
	if name == "." {
		return name, nil
	}
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || strings.ContainsRune(name, ':') {
		return "", fs.ErrInvalid
	}
	parts := strings.Split(strings.ReplaceAll(name, "/", `\`), `\`)
	if len(parts) == 0 {
		return "", fs.ErrInvalid
	}
	clean := make([]string, 0, len(parts))
	for index, part := range parts {
		switch part {
		case "", "..":
			return "", fs.ErrInvalid
		case ".":
			// A leading ./ is a harmless relative spelling. Keeping the
			// rest of the grammar strict prevents malformed paths from being
			// silently repaired into a different capability target.
			if index != 0 {
				return "", fs.ErrInvalid
			}
		default:
			clean = append(clean, part)
		}
	}
	if len(clean) == 0 {
		return "", fs.ErrInvalid
	}
	return strings.Join(clean, `\`), nil
}

// descriptorWindowsUnicodePath creates an NT relative name. In particular,
// NTCreateFile interprets an empty name with RootDirectory as that directory;
// the user-facing "." spelling must therefore be converted before the call.
func descriptorWindowsUnicodePath(path string) (*windows.NTUnicodeString, error) {
	if path == "." {
		path = ""
	}
	return windows.NewNTUnicodeString(path)
}

func descriptorWindowsOpenOptions(request fsapi.DescriptorOpenRequest) (uint32, uint32, error) {
	if request.Directory && request.Create {
		return 0, 0, errors.ErrUnsupported
	}
	disposition := uint32(windows.FILE_OPEN)
	switch {
	case request.Create && request.Exclusive:
		disposition = windows.FILE_CREATE
	case request.Create && request.Truncate:
		disposition = windows.FILE_OVERWRITE_IF
	case request.Create:
		disposition = windows.FILE_OPEN_IF
	case request.Truncate:
		disposition = windows.FILE_OVERWRITE
	}
	options := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT | windows.FILE_OPEN_FOR_BACKUP_INTENT)
	if request.Directory {
		options |= windows.FILE_DIRECTORY_FILE
	}
	return disposition, options, nil
}
