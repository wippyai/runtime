//go:build windows

// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"io/fs"
	"syscall"
)

// On Windows FileInfo.Sys() carries no volume serial or file index, so only
// the modification time is available.
func osFileIdentityImpl(info fs.FileInfo) (fileIdentity, bool) {
	st, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return fileIdentity{}, false
	}
	return fileIdentity{mtimeNs: st.LastWriteTime.Nanoseconds()}, true
}
