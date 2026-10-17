//go:build linux

// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"io/fs"
	"syscall"
)

func osFileIdentityImpl(info fs.FileInfo) (fileIdentity, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, false
	}
	return fileIdentity{
		dev:     uint64(st.Dev), //nolint:unconvert
		ino:     uint64(st.Ino), //nolint:unconvert
		ctimeNs: st.Ctim.Nano(),
		mtimeNs: st.Mtim.Nano(),
		hasDev:  true,
		hasCtim: true,
	}, true
}
