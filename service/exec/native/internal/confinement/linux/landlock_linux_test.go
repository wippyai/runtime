// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLandlockABIProbeDoesNotConstrainParent(t *testing.T) {
	version, err := LandlockABI()
	if err != nil {
		t.Logf("Landlock unavailable on this host: %v", err)
		return
	}
	if version < 1 {
		t.Fatalf("invalid Landlock ABI %d", version)
	}
	// A second probe still works; probing never calls restrict_self.
	second, err := LandlockABI()
	if err != nil || second != version {
		t.Fatalf("Landlock ABI changed after probe: %d, %v", second, err)
	}
}

func TestLandlockRulesDenyOutOfGrantRead(t *testing.T) {
	version, err := LandlockABI()
	if err != nil || version < minimumLandlockABI {
		t.Skipf("Landlock ABI %d unavailable: %v", version, err)
	}
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(allowed, "allowed"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLandlockRuleChild$")
	command.Env = append(os.Environ(),
		"WIPPY_LANDLOCK_TEST_CHILD=1",
		"WIPPY_LANDLOCK_TEST_ALLOWED="+allowed,
		"WIPPY_LANDLOCK_TEST_OUTSIDE="+outside,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restricted child failed: %v\n%s", err, output)
	}
}

func TestLandlockRuleChild(t *testing.T) {
	if os.Getenv("WIPPY_LANDLOCK_TEST_CHILD") != "1" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	allowed := os.Getenv("WIPPY_LANDLOCK_TEST_ALLOWED")
	outside := os.Getenv("WIPPY_LANDLOCK_TEST_OUTSIDE")
	fd, err := unix.Open(allowed, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := InstallLandlock([]LandlockGrant{{FD: fd, Read: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(allowed, "allowed")); err != nil {
		t.Fatal(fmt.Errorf("read inside grant: %w", err))
	}
	if _, err := os.ReadFile(filepath.Join(outside, "secret")); err == nil {
		t.Fatal("read outside Landlock grant succeeded")
	}
	if err := os.WriteFile(filepath.Join(allowed, "created"), []byte("escape"), 0600); err == nil {
		t.Fatal("write inside read-only Landlock grant succeeded")
	}
	if err := os.WriteFile(filepath.Join(outside, "created"), []byte("escape"), 0600); err == nil {
		t.Fatal("write outside Landlock grant succeeded")
	}
	if err := exec.CommandContext(t.Context(), "/bin/true").Run(); err == nil {
		t.Fatal("execute outside Landlock grant succeeded")
	}
}
