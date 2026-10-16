// SPDX-License-Identifier: MPL-2.0

package fs

import "io/fs"

// maxExactFloat is the largest integer a Lua number represents exactly.
const maxExactFloat = 1 << 53

// fileIdentity is the OS-level identity of a file version.
type fileIdentity struct {
	dev     uint64
	ino     uint64
	ctimeNs int64
	mtimeNs int64
	hasDev  bool
	hasCtim bool
}

// osFileIdentity extracts the identity from a FileInfo produced by the real OS
// filesystem. It reports false for virtual filesystems.
func osFileIdentity(info fs.FileInfo) (fileIdentity, bool) {
	return osFileIdentityImpl(info)
}
