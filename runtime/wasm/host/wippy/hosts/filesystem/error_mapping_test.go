// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"io/fs"
	"os"
	"syscall"
	"testing"

	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestFilesystemSpecificErrorsPreserveNativeMeaning(t *testing.T) {
	for _, tc := range []struct {
		err  error
		name string
		want ErrorCode
	}{
		{fsapi.ErrNotDirectory, "not-directory", ErrorNotDirectory},
		{fs.ErrNotExist, "missing", ErrorNoEntry},
		{fsapi.ErrIsDirectory, "is-directory", ErrorIsDirectory},
		{fsapi.ErrNotEmpty, "not-empty", ErrorNotEmpty},
		{syscall.ENOTEMPTY, "native-not-empty", ErrorNotEmpty},
		{fs.ErrExist, "exists", ErrorExist},
		{fsapi.ErrBusy, "busy", ErrorBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Native providers wrap the semantic error with the failing path.
			err := &os.PathError{Op: "open-at", Path: "parent/child", Err: tc.err}
			if got := mapOSError(err); got == nil || got.Code != tc.want {
				t.Fatalf("mapOSError(%v) = %#v, want code %v", err, got, tc.want)
			}
		})
	}
}
