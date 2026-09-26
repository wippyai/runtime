// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// InstallIsolationSeccomp blocks host-global kernel interfaces for every
// confined target. With networkNone it also denies sockets and network I/O;
// the separate empty network namespace prevents routing even if a syscall
// variant is unavailable to the filter on a future kernel.
func InstallIsolationSeccomp(networkNone, filesystemRestricted bool) error {
	var arch uint32
	switch runtime.GOARCH {
	case "amd64":
		arch = unix.AUDIT_ARCH_X86_64
	case "arm64":
		arch = unix.AUDIT_ARCH_AARCH64
	default:
		return errors.New("unsupported seccomp architecture")
	}
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4}, // seccomp_data.arch
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: arch, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, // seccomp_data.nr
	}
	if runtime.GOARCH == "amd64" {
		// x32 uses the same audit architecture but ORs this bit into the
		// syscall number. Kill it instead of allowing an alternate socket ABI.
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, K: 0x40000000, Jf: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		)
	}
	if filesystemRestricted {
		// Landlock does not mediate ioctl-driven filesystem metadata changes
		// (for example FS_IOC_SETFLAGS). After setup the target may only use a
		// small, reviewed terminal/descriptor ioctl set; the helper has already
		// completed its mount and PTY setup before this filter is installed.
		ioctlAllowed := []uint32{
			unix.TCGETS, unix.TCSETS, unix.TCSETSW, unix.TCSETSF,
			unix.TIOCGWINSZ, unix.TIOCSWINSZ,
			unix.TIOCGPGRP, unix.TIOCSPGRP,
		}
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K,
				K: unix.SYS_IOCTL, Jf: uint8(2*len(ioctlAllowed) + 2)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24}, // args[1], ioctl request
		)
		for _, request := range ioctlAllowed {
			filters = append(filters,
				unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: request, Jf: 1},
				unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
			)
		}
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, // restore syscall number
		)
	}
	denied := []uint32{
		unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
		unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_FANOTIFY_INIT,
		unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT,
		unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_PIDFD_OPEN, unix.SYS_PIDFD_GETFD, unix.SYS_PIDFD_SEND_SIGNAL,
		unix.SYS_KCMP, unix.SYS_USERFAULTFD,
		unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT,
		unix.SYS_SETNS, unix.SYS_UNSHARE,
		unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER,
		unix.SYS_CLOCK_SETTIME, unix.SYS_SETTIMEOFDAY, unix.SYS_ADJTIMEX,
	}
	if filesystemRestricted {
		denied = append(denied,
			// Landlock grants file-content writes, but it cannot selectively
			// mediate these metadata changes. Deny them for every path until a
			// path-aware mechanism is available; fs.write does not imply them.
			unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
			unix.SYS_FCHOWN, unix.SYS_FCHOWNAT,
			unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR,
			unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR,
			unix.SYS_SETXATTRAT, unix.SYS_REMOVEXATTRAT,
			unix.SYS_UTIMENSAT)
		denied = append(denied, legacyMetadataSyscalls()...)
	}
	if networkNone {
		denied = append(denied,
			unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT,
			unix.SYS_BIND, unix.SYS_LISTEN, unix.SYS_ACCEPT, unix.SYS_ACCEPT4,
			unix.SYS_SENDTO, unix.SYS_SENDMSG, unix.SYS_SENDMMSG,
			unix.SYS_RECVFROM, unix.SYS_RECVMSG, unix.SYS_RECVMMSG,
		)
	}
	for _, number := range denied {
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		)
	}
	filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER,
		uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
		return fmt.Errorf("install isolation seccomp: %w", err)
	}
	return nil
}
