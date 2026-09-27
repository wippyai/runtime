// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LandlockGrant refers to an already-open, pinned directory. The installer
// never resolves a path string supplied by the launch caller.
type LandlockGrant struct {
	FD       int
	Read     bool
	Write    bool
	Exec     bool
	FileOnly bool
	IOCTLDev bool
}

const (
	landlockRead = unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR
	landlockWrite = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
	landlockHandled = landlockRead | landlockWrite |
		unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
)

// InstallLandlock restricts the calling OS thread and all processes it later
// executes. The dedicated helper must lock that thread before calling and
// remain on it until exec; this function must never run in the runtime.
// The helper must close every authority-bearing inherited descriptor before
// target exec; Landlock does not revoke descriptors opened before restriction.
func InstallLandlock(grants []LandlockGrant) error {
	if err := requireLandlockABI(); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	ruleset := unix.LandlockRulesetAttr{Access_fs: landlockHandled}
	// ABI 5 supports handled_access_fs and handled_access_net but not the
	// scoped field added later. Pass only the supported prefix of the struct.
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&ruleset)), unsafe.Offsetof(ruleset.Scoped), 0)
	if errno != 0 {
		return fmt.Errorf("create Landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))

	for _, grant := range grants {
		if grant.FD < 0 {
			return fmt.Errorf("invalid Landlock grant descriptor")
		}
		var rights uint64
		if grant.FileOnly {
			if grant.Read || grant.Write {
				rights |= unix.LANDLOCK_ACCESS_FS_READ_FILE
			}
			if grant.Write {
				rights |= unix.LANDLOCK_ACCESS_FS_WRITE_FILE
			}
			if grant.IOCTLDev {
				rights |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
			}
		} else {
			if grant.Read || grant.Write {
				rights |= landlockRead
			}
			if grant.Write {
				rights |= landlockWrite
			}
		}
		if grant.Exec {
			rights |= unix.LANDLOCK_ACCESS_FS_EXECUTE
		}
		if rights == 0 {
			continue
		}
		attr := unix.LandlockPathBeneathAttr{
			Allowed_access: rights,
			Parent_fd:      int32(grant.FD),
		}
		_, _, errno = unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE,
			fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
		if errno != 0 {
			return fmt.Errorf("add Landlock path rule: %w", errno)
		}
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("install Landlock ruleset: %w", errno)
	}
	return nil
}
