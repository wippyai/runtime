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

// BoundDirectory freezes the object named by an entry path. The declared
// spelling may contain a normal filesystem symlink (such as /lib64 ->
// /usr/lib64); the canonical source is pinned and passed to the child while
// the declared spelling remains the target path in its private view.
type BoundDirectory struct {
	root      *WorkDirRoot
	Declared  string
	Canonical string
}

func BindDeclaredDirectory(path string) (*BoundDirectory, error) {
	if !cleanAbsolute(path) {
		return nil, fmt.Errorf("%w: declared directory must be clean and absolute", confinement.ErrInvalid)
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("bind declared directory: %w", err)
	}
	canonical, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil || !cleanAbsolute(canonical) || strings.HasSuffix(canonical, " (deleted)") {
		_ = unix.Close(fd)
		if err != nil {
			return nil, fmt.Errorf("resolve declared directory: %w", err)
		}
		return nil, fmt.Errorf("%w: unresolvable declared directory", confinement.ErrInvalid)
	}
	check, err := unix.Openat2(unix.AT_FDCWD, canonical, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("verify canonical directory: %w", err)
	}
	same, compareErr := SameOpenDirectoryFDs(fd, check)
	_ = unix.Close(check)
	if compareErr != nil || !same {
		_ = unix.Close(fd)
		if compareErr != nil {
			return nil, fmt.Errorf("compare canonical directory identity: %w", compareErr)
		}
		return nil, fmt.Errorf("%w: canonical directory identity changed", confinement.ErrInvalid)
	}
	if err := rejectSpecialHostSource(canonical, fd); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &BoundDirectory{
		Declared: path, Canonical: canonical,
		root: &WorkDirRoot{path: canonical, fd: fd},
	}, nil
}

func rejectSpecialHostSource(canonical string, fd int) error {
	for _, prefix := range []string{"/dev", "/proc", "/sys"} {
		if canonical == prefix || strings.HasPrefix(canonical, prefix+"/") {
			return fmt.Errorf("%w: special host filesystem grant", confinement.ErrInvalid)
		}
	}
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return fmt.Errorf("inspect grant filesystem: %w", err)
	}
	switch stat.Type {
	case unix.PROC_SUPER_MAGIC, unix.SYSFS_MAGIC, unix.CGROUP_SUPER_MAGIC,
		unix.CGROUP2_SUPER_MAGIC, unix.DEVPTS_SUPER_MAGIC:
		return fmt.Errorf("%w: special host filesystem grant", confinement.ErrInvalid)
	}
	return nil
}

func (b *BoundDirectory) OpenDescendant(requested string) (*os.File, string, error) {
	if !cleanAbsolute(requested) {
		return nil, "", fmt.Errorf("%w: descendant must be clean and absolute", confinement.ErrInvalid)
	}
	rel, err := filepath.Rel(b.Declared, requested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, "", ErrOutsideRoot
	}
	canonical := filepath.Join(b.Canonical, rel)
	file, err := b.root.OpenBoundWorkDir(canonical)
	return file, canonical, err
}

func (b *BoundDirectory) Close() error { return b.root.Close() }
