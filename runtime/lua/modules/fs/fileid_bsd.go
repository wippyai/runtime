//go:build darwin || freebsd

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
		dev:     uint64(uint32(st.Dev)), //nolint:unconvert,gosec
		ino:     uint64(st.Ino),         //nolint:unconvert
		ctimeNs: st.Ctimespec.Nano(),
		mtimeNs: st.Mtimespec.Nano(),
		hasDev:  true,
		hasCtim: true,
	}, true
}
