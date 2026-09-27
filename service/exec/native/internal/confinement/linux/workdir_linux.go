// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	"golang.org/x/sys/unix"
)

var ErrOutsideRoot = errors.New("working directory is outside approved root")

// workDirRoot pins an entry-owned Linux directory object. Holding its descriptor
// prevents a later replacement of the configured path from changing the
// authority of an executor that has already admitted the entry.
type workDirRoot struct {
	path string
	fd   int
	mu   sync.RWMutex
}

// openBoundWorkDir opens a caller's requested directory relative to the
// already pinned root. The returned descriptor, not requested's spelling,
// must be used by the child for fchdir. Callers retain it until child setup.
func (r *workDirRoot) openBoundWorkDir(requested string) (*os.File, error) {
	if !cleanAbsolute(requested) {
		return nil, fmt.Errorf("%w: work_dir must be clean and absolute", confinement.ErrInvalid)
	}
	rel, err := filepath.Rel(r.path, requested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, ErrOutsideRoot
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.fd < 0 {
		return nil, os.ErrClosed
	}
	fd, err := unix.Openat2(r.fd, rel, &unix.OpenHow{
		Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("bind work_dir: %w", err)
	}
	return os.NewFile(uintptr(fd), requested), nil
}

func (r *workDirRoot) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}
