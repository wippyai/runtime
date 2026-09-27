// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const lockedTargetSecureBits = (1 << 0) | (1 << 1) | // NOROOT and its lock
	(1 << 2) | (1 << 3) | // NO_SETUID_FIXUP and its lock
	(1 << 6) | (1 << 7) // NO_CAP_AMBIENT_RAISE and its lock

// InstallUnrestrictedKernelView preserves the ordinary host filesystem while
// replacing the two kernel-control filesystems that must never remain ambient
// target authority. The caller is already in a private mount/PID namespace.
func InstallUnrestrictedKernelView() error {
	// Stop host mount propagation before validating topology; otherwise a new
	// procfs or cgroupfs alias could appear between validation and masking.
	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("make unrestricted mount namespace private: %w", err)
	}
	mountInfo, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("open mount topology: %w", err)
	}
	err = validateKernelMountTopology(mountInfo)
	_ = mountInfo.Close()
	if err != nil {
		return err
	}
	// This proc instance is attached to the confined PID namespace. subset=pid
	// also omits host-global sysctl files; dumpability protects PID 1's own
	// sensitive entries from the same-UID target.
	if err := unix.Mount("proc", "/proc", "proc",
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "hidepid=2,subset=pid"); err != nil {
		return fmt.Errorf("mount confined proc: %w", err)
	}
	// The delegated host cgroup is writable by the runtime UID. Hide it rather
	// than exposing a read-only alias: this prevents both limit changes and
	// migration into a sibling cgroup.
	if err := unix.Mount("tmpfs", "/sys/fs/cgroup", "tmpfs",
		unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "mode=0555,size=4096"); err != nil {
		return fmt.Errorf("mask host cgroup controls: %w", err)
	}
	return nil
}

func validateKernelMountTopology(reader io.Reader) error {
	hasProcRoot := false
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if len(fields) < 6 || separator < 0 || separator+1 >= len(fields) {
			return errors.New("invalid mount topology")
		}
		mountpoint := unescapeMountInfoPath(fields[4])
		switch fields[separator+1] {
		case "proc":
			if mountpoint != "/proc" && !strings.HasPrefix(mountpoint, "/proc/") {
				return fmt.Errorf("proc mount outside /proc: %s", mountpoint)
			}
			hasProcRoot = hasProcRoot || mountpoint == "/proc"
		case "cgroup", "cgroup2":
			if mountpoint != "/sys/fs/cgroup" && !strings.HasPrefix(mountpoint, "/sys/fs/cgroup/") {
				return fmt.Errorf("cgroup mount outside /sys/fs/cgroup: %s", mountpoint)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read mount topology: %w", err)
	}
	if !hasProcRoot {
		return errors.New("missing /proc mount")
	}
	return nil
}

func unescapeMountInfoPath(path string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(path)
}

// DropTargetCapabilities makes UID 0 inside the launch user namespace an
// ordinary UID across exec. no_new_privs is already set, but securebits and an
// empty bounding set also prevent root and file-capability regain paths.
func KernelLastCapability() (int, error) {
	value, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err != nil {
		return 0, fmt.Errorf("read kernel capability ceiling: %w", err)
	}
	last, err := strconv.Atoi(strings.TrimSpace(string(value)))
	if err != nil || last < 0 || last > 255 {
		return 0, errors.New("invalid kernel capability ceiling")
	}
	return last, nil
}

func DropTargetCapabilities(lastCapability int) error {
	if err := unix.Prctl(unix.PR_SET_SECUREBITS, lockedTargetSecureBits, 0, 0, 0); err != nil {
		return fmt.Errorf("lock target securebits: %w", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear target ambient capabilities: %w", err)
	}
	for capability := 0; capability <= lastCapability; capability++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); err != nil {
			return fmt.Errorf("drop target bounding capability %d: %w", capability, err)
		}
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return fmt.Errorf("clear target capabilities: %w", err)
	}
	return nil
}
