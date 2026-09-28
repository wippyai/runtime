// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func main() {
	// An omitted fs policy is genuinely unrestricted; network-only
	// confinement must not accidentally install the metadata seccomp rules.
	outside := os.Getenv("OUTSIDE")
	probe := filepath.Join(outside, "network-only-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
		os.Exit(70)
	}
	defer os.Remove(probe)
	if err := os.Chmod(probe, 0640); err != nil {
		os.Exit(71)
	}
	if os.Getenv("TMPDIR") != "" {
		os.Exit(72)
	}
	// The unrestricted host filesystem must not include ambient authority over
	// the trusted namespace supervisor or the runtime's delegated cgroup.
	for _, path := range []string{"/proc/1/mem", "/proc/1/fd/6", "/proc/1/root", "/proc/1/cwd"} {
		if file, err := os.Open(path); err == nil {
			_ = file.Close()
			os.Exit(74)
		}
	}
	if err := os.WriteFile("/sys/fs/cgroup/escape", []byte("0"), 0600); !errors.Is(err, syscall.EROFS) {
		os.Exit(76)
	}
	status, err := os.Open("/proc/self/status")
	if err != nil {
		os.Exit(77)
	}
	scanner := bufio.NewScanner(status)
	capabilitiesZero := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.HasPrefix(fields[0], "Cap") && fields[1] == "0000000000000000" {
			capabilitiesZero++
		}
	}
	_ = status.Close()
	if scanner.Err() != nil || capabilitiesZero != 5 {
		os.Exit(78)
	}
	const expectedSecureBits = 0xcf
	secureBits, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, unix.PR_GET_SECUREBITS, 0, 0, 0, 0, 0)
	if errno != 0 || secureBits != expectedSecureBits {
		os.Exit(81)
	}
	if err := syscall.Mount("tmpfs", outside, "tmpfs", 0, ""); !errors.Is(err, syscall.EPERM) {
		os.Exit(79)
	}
	for _, number := range []uintptr{
		unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_FSPICK,
		unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT, unix.SYS_MOUNT_SETATTR,
	} {
		_, _, errno := unix.RawSyscall6(number, 0, 0, 0, 0, 0, 0)
		if errno != unix.EPERM {
			os.Exit(82)
		}
	}
	for _, family := range []int{syscall.AF_INET, syscall.AF_INET6, syscall.AF_UNIX} {
		fd, err := syscall.Socket(family, syscall.SOCK_STREAM, 0)
		if err == nil {
			_ = syscall.Close(fd)
			os.Exit(80)
		}
	}
	if os.Getenv("WIPPY_CONFINE_REEXEC") == "" {
		environment := append(os.Environ(), "WIPPY_CONFINE_REEXEC=1")
		if err := syscall.Exec(os.Args[0], os.Args, environment); err != nil {
			os.Exit(83)
		}
	}
	fmt.Println("network-only-ok")
}
