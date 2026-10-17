//go:build !linux && !darwin && !freebsd && !windows

// SPDX-License-Identifier: MPL-2.0

package fs

import "io/fs"

func osFileIdentityImpl(fs.FileInfo) (fileIdentity, bool) {
	return fileIdentity{}, false
}
