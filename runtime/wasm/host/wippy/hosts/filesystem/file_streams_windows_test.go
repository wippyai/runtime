//go:build windows

// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"os"

	"golang.org/x/sys/windows"
)

// openRetainedTestFile grants delete sharing to model a native descriptor
// handle. os.OpenFile does not request FILE_SHARE_DELETE on Windows, which
// would make the replacement checks fail before retainedFile can be tested.
func openRetainedTestFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
