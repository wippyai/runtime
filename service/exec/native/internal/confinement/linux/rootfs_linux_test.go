// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPrivateMountAndNetworkView(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(allowed, "visible"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(allowed, "escape")); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPrivateMountViewChild$")
	command.Env = append(os.Environ(),
		"WIPPY_MOUNT_VIEW_CHILD=1",
		"WIPPY_MOUNT_VIEW_ALLOWED="+allowed,
		"WIPPY_MOUNT_VIEW_ROOT="+root,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: unix.CLONE_NEWUSER | unix.CLONE_NEWNS | unix.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getuid(), Size: 1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getgid(), Size: 1,
		}},
		GidMappingsEnableSetgroups: false,
	}
	output, err := command.CombinedOutput()
	if err != nil {
		skipHostConfinementUnavailable(t, "user/mount/network namespaces unavailable", err)
		t.Fatalf("private mount child failed: %v\n%s", err, output)
	}
}

func TestPrivateMountViewChild(t *testing.T) {
	if os.Getenv("WIPPY_MOUNT_VIEW_CHILD") != "1" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	allowed := os.Getenv("WIPPY_MOUNT_VIEW_ALLOWED")
	root := os.Getenv("WIPPY_MOUNT_VIEW_ROOT")
	fd, err := unix.Open(allowed, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := InstallMountView(root, []PinnedMount{{
		Target: "/grant", FD: fd, ReadOnly: true, NoExec: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile("/grant/visible"); err != nil || string(data) != "ok" {
		t.Fatalf("approved path was not mounted: %q, %v", data, err)
	}
	if _, err := os.ReadFile("/grant/escape"); err == nil {
		t.Fatal("symlink escaped the private filesystem view")
	}
	if _, err := os.ReadFile("/etc/passwd"); err == nil {
		t.Fatal("host filesystem remained visible")
	}
	if err := os.WriteFile("/grant/created", []byte("escape"), 0600); err == nil {
		t.Fatal("read-only bind mount accepted a write")
	}
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(t.Context(), "tcp", "1.1.1.1:53")
	if err == nil {
		_ = conn.Close()
		t.Fatal("isolated network namespace reached the host network")
	}
}
