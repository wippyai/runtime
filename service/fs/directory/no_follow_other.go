//go:build !unix

package directory

import (
	"errors"
	fsapi "github.com/wippyai/runtime/api/fs"
	"io/fs"
)

// OpenFileNoFollow fails explicitly where no atomic implementation is supplied.
func (d *FS) OpenFileNoFollow(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	return nil, &fs.PathError{Op: "open-nofollow", Path: name, Err: errors.ErrUnsupported}
}

func (d *FS) OpenDirectory(name string, noFollow bool) (fs.File, error) {
	return nil, &fs.PathError{Op: "open-directory", Path: name, Err: errors.ErrUnsupported}
}
