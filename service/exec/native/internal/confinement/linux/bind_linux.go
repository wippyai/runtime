// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"fmt"
	"os"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	"golang.org/x/sys/unix"
)

// BindDirectory resolves a declared grant exactly once. Symlinks, including
// magic links, are not admitted at any path component. The returned handle is
// the authority to pass to a launch helper; re-opening path by name later is
// not an equivalent operation.
func BindDirectory(path string) (*os.File, error) {
	if !cleanAbsolute(path) || path == "/" {
		return nil, fmt.Errorf("%w: grant must be a clean non-root absolute directory", confinement.ErrInvalid)
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("bind declared directory: %w", err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// SameDirectory checks the object identity of a path reached inside the
// private mount view against the parent's pinned handle. The helper uses this
// before chdir so a swapped path cannot substitute another working directory.
func SameDirectory(pinnedFD int, mountedPath string) (bool, error) {
	if !cleanAbsolute(mountedPath) {
		return false, fmt.Errorf("%w: invalid mounted directory", confinement.ErrInvalid)
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, mountedPath, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	return SameOpenDirectoryFDs(pinnedFD, fd)
}

// SameOpenDirectoryFDs compares two open directory objects without looking up
// either path again. The caller must hold both descriptors through its use.
func SameOpenDirectoryFDs(pinnedFD, candidateFD int) (bool, error) {
	var pinned, mounted unix.Stat_t
	if err := unix.Fstat(pinnedFD, &pinned); err != nil {
		return false, err
	}
	if err := unix.Fstat(candidateFD, &mounted); err != nil {
		return false, err
	}
	return pinned.Dev == mounted.Dev && pinned.Ino == mounted.Ino, nil
}
