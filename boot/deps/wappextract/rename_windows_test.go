// SPDX-License-Identifier: MPL-2.0

//go:build windows

package wappextract

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/wapp"
	"golang.org/x/sys/windows"
)

func setRenameRetryTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := renameRetryTimeout
	renameRetryTimeout = timeout
	t.Cleanup(func() { renameRetryTimeout = previous })
}

func failOSRename(t *testing.T, times int, errno error) *int {
	t.Helper()
	previous := osRename
	calls := 0
	osRename = func(oldpath, newpath string) error {
		calls++
		if times < 0 || calls <= times {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errno}
		}
		return previous(oldpath, newpath)
	}
	t.Cleanup(func() { osRename = previous })
	return &calls
}

func makeDir(t *testing.T, dir string) (string, string) {
	t.Helper()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(src, "file.lua")
	if err := os.WriteFile(file, []byte("return true"), 0600); err != nil {
		t.Fatal(err)
	}
	return src, file
}

func TestRenameDirRetriesTransientErrors(t *testing.T) {
	for _, errno := range []error{windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION} {
		t.Run(errno.Error(), func(t *testing.T) {
			dir := t.TempDir()
			src, _ := makeDir(t, dir)
			calls := failOSRename(t, 3, errno)

			dst := filepath.Join(dir, "dst")
			if err := RenameDir(src, dst); err != nil {
				t.Fatalf("RenameDir failed: %v", err)
			}
			if *calls != 4 {
				t.Fatalf("rename calls = %d, want 4", *calls)
			}
			if _, err := os.Stat(filepath.Join(dst, "file.lua")); err != nil {
				t.Fatalf("renamed file missing: %v", err)
			}
		})
	}
}

func TestRenameDirStopsAfterTimeout(t *testing.T) {
	setRenameRetryTimeout(t, 100*time.Millisecond)
	dir := t.TempDir()
	src, file := makeDir(t, dir)
	failOSRename(t, -1, windows.ERROR_SHARING_VIOLATION)

	started := time.Now()
	err := RenameDir(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("RenameDir err = %v, want sharing violation", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("RenameDir took %v, want bounded retry", elapsed)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("source must remain: %v", err)
	}
}

func TestRenameDirDoesNotRetryPermanentErrors(t *testing.T) {
	dir := t.TempDir()
	src, _ := makeDir(t, dir)
	calls := failOSRename(t, -1, windows.ERROR_PATH_NOT_FOUND)

	if err := RenameDir(src, filepath.Join(dir, "dst")); !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		t.Fatalf("RenameDir err = %v, want path not found", err)
	}
	if *calls != 1 {
		t.Fatalf("rename calls = %d, want 1", *calls)
	}
}

func TestExtractWappToDirRetriesTransientActivationErrors(t *testing.T) {
	dir := t.TempDir()
	wappPath := filepath.Join(dir, "mod.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("app", "svc"),
		Kind: "service",
		Data: map[string]any{"ok": true},
	}}, nil)
	targetDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	failOSRename(t, 2, windows.ERROR_ACCESS_DENIED)

	if err := ExtractWappToDirKeepSource(wappPath, targetDir); err != nil {
		t.Fatalf("activation with transient errors failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "_index.yaml")); err != nil {
		t.Fatalf("extracted index missing: %v", err)
	}
	assertOnlyEntries(t, dir, "mod", "mod.wapp")
}

func holdFile(t *testing.T, path string) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return handle
}

func requireHandleBlocksDirectoryRename(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	src, file := makeDir(t, dir)
	handle := holdFile(t, file)
	defer windows.CloseHandle(handle)
	if err := os.Rename(src, filepath.Join(dir, "probe")); err == nil {
		t.Skip("platform does not block a directory rename while a file handle is open")
	}
}

func TestExtractWappToDirActivatesWhileNewTreeIsBrieflyHeld(t *testing.T) {
	requireHandleBlocksDirectoryRename(t)
	dir := t.TempDir()
	wappPath := filepath.Join(dir, "mod.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("app", "svc"),
		Kind: "service",
		Data: map[string]any{"ok": true},
	}}, nil)
	targetDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	err := ExtractWappToDirKeepSourceWith(wappPath, targetDir, func(tree string) error {
		handle := holdFile(t, filepath.Join(tree, "_index.yaml"))
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = windows.CloseHandle(handle)
			close(done)
		}()
		return nil
	})
	<-done
	if err != nil {
		t.Fatalf("activation with briefly held new tree failed: %v", err)
	}
	assertOnlyEntries(t, dir, "mod", "mod.wapp")
}

func TestExtractWappToDirHeldDestinationKeepsPrevious(t *testing.T) {
	requireHandleBlocksDirectoryRename(t)
	setRenameRetryTimeout(t, 100*time.Millisecond)
	dir := t.TempDir()
	wappPath := filepath.Join(dir, "mod.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("app", "svc"),
		Kind: "service",
		Data: map[string]any{"ok": true},
	}}, nil)
	targetDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	previousPath := filepath.Join(targetDir, "previous.lua")
	if err := os.WriteFile(previousPath, []byte("return true"), 0600); err != nil {
		t.Fatal(err)
	}

	handle := holdFile(t, previousPath)
	err := ExtractWappToDirKeepSource(wappPath, targetDir)
	_ = windows.CloseHandle(handle)
	if err == nil || !strings.Contains(err.Error(), "move existing directory aside") {
		t.Fatalf("err = %v, want move aside failure", err)
	}
	if _, err := os.Stat(previousPath); err != nil {
		t.Fatalf("previous tree must remain usable: %v", err)
	}
	assertOnlyEntries(t, dir, "mod", "mod.wapp")

	if err := ExtractWappToDirKeepSource(wappPath, targetDir); err != nil {
		t.Fatalf("activation after release failed: %v", err)
	}
	assertOnlyEntries(t, dir, "mod", "mod.wapp")
}
