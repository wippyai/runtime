// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	"golang.org/x/sys/unix"
)

// PinnedMount binds an already-open directory into a fresh mount namespace.
// Target is its absolute path inside the private root. The caller owns FD.
type PinnedMount struct {
	Target   string
	FD       int
	ReadOnly bool
	NoExec   bool
}

// InstallMountView builds a private tmpfs root and pivots into it. The caller
// must already be in a new user and mount namespace, on the setup thread, and
// must execute the target without returning to runtime code. Only pinned
// objects enter the view; no host /proc, /sys, /dev or socket path is added.
func InstallMountView(root string, mounts []PinnedMount) error {
	return InstallMountViewWithPrivateExec(root, mounts, false)
}

// InstallStandalonePrivateHome overlays a runtime-created empty directory
// with a launch-private tmpfs in the child's mount namespace. This supports a
// private HOME when the rest of the host filesystem is intentionally
// unrestricted: neither the host nor another launch can observe its contents.
func InstallStandalonePrivateHome(root string) error {
	if !cleanAbsolute(root) || root == "/" {
		return fmt.Errorf("%w: invalid standalone private home", confinement.ErrInvalid)
	}
	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount namespace private: %w", err)
	}
	if err := unix.Mount("tmpfs", root, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		"mode=0700"); err != nil {
		return fmt.Errorf("mount standalone private home: %w", err)
	}
	if err := os.Mkdir(filepath.Join(root, "home"), 0700); err != nil {
		return fmt.Errorf("create standalone private home: %w", err)
	}
	return nil
}

// InstallMountViewWithPrivateExec permits execution from private tmpfs only
// when the normalized policy explicitly granted it. Landlock still mediates
// executable entry for each private directory.
func InstallMountViewWithPrivateExec(root string, mounts []PinnedMount, privateExec bool) error {
	if !cleanAbsolute(root) || root == "/" {
		return fmt.Errorf("%w: invalid private root", confinement.ErrInvalid)
	}
	for _, mount := range mounts {
		if !cleanAbsolute(mount.Target) || mount.Target == "/" || mount.FD < 0 ||
			mount.Target == "/dev" || strings.HasPrefix(mount.Target, "/dev/") ||
			mount.Target == "/proc" || strings.HasPrefix(mount.Target, "/proc/") {
			return fmt.Errorf("%w: invalid bind mount", confinement.ErrInvalid)
		}
	}
	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount namespace private: %w", err)
	}
	rootFlags := uintptr(unix.MS_NOSUID | unix.MS_NODEV)
	if !privateExec {
		rootFlags |= unix.MS_NOEXEC
	}
	if err := unix.Mount("tmpfs", root, "tmpfs", rootFlags, "mode=0700"); err != nil {
		return fmt.Errorf("mount private root: %w", err)
	}
	if err := installPrivateDevices(root); err != nil {
		return err
	}
	for _, private := range []string{confinement.PrivateHomePath, confinement.PrivateTempPath} {
		if err := os.MkdirAll(filepath.Join(root, strings.TrimPrefix(private, "/")), 0700); err != nil {
			return fmt.Errorf("create private directory: %w", err)
		}
	}
	// Build every mountpoint before a read-only parent is installed.
	for _, mount := range mounts {
		if err := os.MkdirAll(filepath.Join(root, strings.TrimPrefix(mount.Target, "/")), 0700); err != nil {
			return fmt.Errorf("create private mountpoint: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".old"), 0700); err != nil {
		return fmt.Errorf("create old-root mountpoint: %w", err)
	}
	for _, mount := range mounts {
		target := filepath.Join(root, strings.TrimPrefix(mount.Target, "/"))
		source := fmt.Sprintf("/proc/self/fd/%d", mount.FD)
		if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
			resolved, _ := os.Readlink(source)
			return fmt.Errorf("bind pinned directory %s (%s) to %s: %w", source, resolved, mount.Target, err)
		}
		flags := uintptr(unix.MS_BIND | unix.MS_REMOUNT | unix.MS_NOSUID | unix.MS_NODEV)
		if mount.ReadOnly {
			flags |= unix.MS_RDONLY
		}
		if mount.NoExec {
			flags |= unix.MS_NOEXEC
		}
		if err := unix.Mount("", target, "", flags, ""); err != nil {
			return fmt.Errorf("set bind mount rights: %w", err)
		}
	}
	if err := unix.PivotRoot(root, filepath.Join(root, ".old")); err != nil {
		return fmt.Errorf("pivot to private root: %w", err)
	}
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("enter private root: %w", err)
	}
	if err := unix.Unmount("/.old", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("detach host root: %w", err)
	}
	if err := unix.Rmdir("/.old"); err != nil {
		return fmt.Errorf("remove old-root mountpoint: %w", err)
	}
	return nil
}

func installPrivateDevices(root string) error {
	dev := filepath.Join(root, "dev")
	if err := os.MkdirAll(dev, 0755); err != nil {
		return fmt.Errorf("create private device directory: %w", err)
	}
	if err := unix.Mount("tmpfs", dev, "tmpfs", unix.MS_NOSUID|unix.MS_NOEXEC, "mode=0755,size=64k"); err != nil {
		return fmt.Errorf("mount private device directory: %w", err)
	}
	for _, device := range []struct {
		name         string
		major, minor uint32
	}{
		{"null", 1, 3}, {"zero", 1, 5}, {"random", 1, 8},
		{"urandom", 1, 9}, {"tty", 5, 0},
	} {
		path := filepath.Join(dev, device.name)
		if err := unix.Mknod(path, unix.S_IFCHR|0666, int(unix.Mkdev(device.major, device.minor))); err != nil {
			if err := bindSafeHostDevice(path, device.name, device.major, device.minor); err != nil {
				return fmt.Errorf("create private %s device: %w", device.name, err)
			}
		}
	}
	return nil
}

func bindSafeHostDevice(target, name string, major, minor uint32) error {
	source := filepath.Join("/dev", name)
	fd, err := unix.Open(source, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFCHR || stat.Rdev != unix.Mkdev(major, minor) {
		return fmt.Errorf("unsafe host device identity for %s", name)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	_ = file.Close()
	return unix.Mount(fmt.Sprintf("/proc/self/fd/%d", fd), target, "", unix.MS_BIND, "")
}
