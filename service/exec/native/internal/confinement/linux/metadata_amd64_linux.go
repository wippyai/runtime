// SPDX-License-Identifier: MPL-2.0

//go:build linux && amd64

package linux

import "golang.org/x/sys/unix"

func legacyMetadataSyscalls() []uint32 {
	return []uint32{
		unix.SYS_CHMOD, unix.SYS_CHOWN, unix.SYS_LCHOWN,
		unix.SYS_UTIME, unix.SYS_UTIMES, unix.SYS_FUTIMESAT,
	}
}
