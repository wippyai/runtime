//go:build unix

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const ownerSafeSupported = true
const maxLinkDepth = 40

// resolveLinkTarget preserves dot/parent traversal after symlink expansion.
// No file contents are read. A repeated resolution state identifies a loop.
func resolveLinkTarget(path string) (string, error) {
	pending := strings.Split(path, "/")
	resolved := "/"
	seen := make(map[string]bool)
	depth := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(next)
		if err != nil {
			if depth > 0 {
				return "", &linkPolicyRefusal{err}
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			state := next + "\x00" + strings.Join(pending, "/")
			if seen[state] {
				return "", fmt.Errorf("owner_safe: %s: symlink loop", next)
			}
			seen[state] = true
			depth++
			if depth > maxLinkDepth {
				return "", fmt.Errorf("owner_safe: %s: symlink depth exceeds %d", next, maxLinkDepth)
			}
			target, err := os.Readlink(next)
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(target) {
				resolved = "/"
			}
			pending = append(strings.Split(target, "/"), pending...)
		} else {
			if len(pending) > 0 && !info.IsDir() {
				return "", fmt.Errorf("owner_safe: %s: not a directory", next)
			}
			resolved = next
		}
	}
	return resolved, nil
}

// OpenSSH misc.c safe_path ownership/mode rule, applied through filesystem root.
func checkSafeLinkFacts(path string, mode fs.FileMode, uid uint32) error {
	if uid != 0 && uid != uint32(os.Getuid()) {
		return fmt.Errorf("owner_safe: %s: owner uid %d is neither process uid %d nor root", path, uid, os.Getuid())
	}
	if mode&022 != 0 {
		return fmt.Errorf("owner_safe: %s: group/other-writable mode %04o (sticky does not exempt)", path, mode&07777)
	}
	return nil
}

// openOwnerSafe opens the resolved path one component at a time with NOFOLLOW,
// retaining each canonical parent. Checks apply to those exact descriptors,
// including the final file; a symlink swap cannot redirect the checked open.
func (d *FS) openOwnerSafe(name string) (file *os.File, refusal error) {
	defer func() {
		if refusal != nil && !errors.Is(refusal, fs.ErrNotExist) {
			var marked *linkPolicyRefusal
			if !errors.As(refusal, &marked) {
				refusal = &linkPolicyRefusal{refusal}
			}
		}
	}()
	if !filepath.IsLocal(name) {
		return nil, &fs.PathError{Op: "owner_safe", Path: name, Err: fs.ErrInvalid}
	}
	canonical, err := resolveLinkTarget(d.dirPath + "/" + name)
	if err != nil {
		return nil, err
	}
	root, err := resolveLinkTarget(d.dirPath)
	if err != nil {
		return nil, err
	}
	// Contained targets use the host's existing root authority. The retained
	// os.Root fences the open even if a component changes after resolution.
	if relative, err := filepath.Rel(root, canonical); err == nil && filepath.IsLocal(relative) {
		file, err := d.root.Open(relative)
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, fmt.Errorf("owner_safe: %s: not a regular file", canonical)
		}
		return file, nil
	}
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "owner_safe", Path: "/", Err: err}
	}
	opened := []int{current}
	defer func() {
		for _, fd := range opened {
			_ = unix.Close(fd)
		}
	}()
	check := func(fd int, path string, regular bool) error {
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			return &fs.PathError{Op: "owner_safe", Path: path, Err: err}
		}
		if regular && st.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("owner_safe: %s: not a regular file", path)
		}
		return checkSafeLinkFacts(path, fs.FileMode(st.Mode), st.Uid)
	}
	if err := check(current, "/", false); err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(canonical, "/"), "/")
	path := "/"
	for i, part := range parts {
		path = filepath.Join(path, part)
		final := i == len(parts)-1
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if !final {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(current, part, flags, 0)
		if err != nil {
			return nil, &fs.PathError{Op: "owner_safe", Path: path, Err: err}
		}
		if err := check(fd, path, final); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if final {
			return os.NewFile(uintptr(fd), canonical), nil
		}
		opened = append(opened, fd)
		current = fd
	}
	return nil, fmt.Errorf("owner_safe: %s: not a regular file", canonical)
}
