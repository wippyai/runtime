//go:build windows

// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDescriptorUnlinkReportsWindowsSharingViolation(t *testing.T) {
	host, _, descriptor, root := newDirectoryDescriptorHost(t)
	path := root + "/busy"
	if err := os.WriteFile(path, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	// An external handle deliberately denies delete sharing. The native
	// descriptor operation must report busy and leave the file untouched.
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if got := host.MethodDescriptorUnlinkFileAt(context.Background(), descriptor, "busy"); got == nil || got.Code != ErrorBusy {
		t.Fatalf("unlink externally open file = %#v, want busy", got)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "retained" {
		t.Fatalf("failed unlink changed contents: %q, %v", data, err)
	}
}

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
