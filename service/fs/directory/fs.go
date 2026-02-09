package directory

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	fsapi "github.com/wippyai/runtime/api/fs"
	systemfs "github.com/wippyai/runtime/system/fs"
)

var _ fsapi.FS = (*FS)(nil)

// Permission flags for filesystem operations.
type permCheck uint32

const (
	permRead permCheck = 1 << iota
	permWrite
	permExec
)

// FS implements both ReadFS and WriteFS interfaces.
type FS struct {
	root    *os.Root
	dirPath string // original path for error messages
	mode    fs.FileMode
	closed  atomic.Bool
}

// RootPath returns the absolute host path backing this filesystem.
func (d *FS) RootPath() string {
	return d.dirPath
}

// NewFS creates a new FS instance. It automatically adds execute bits
// if the read bits are set but the execute bits are missing.
func NewFS(dirPath string, mode fs.FileMode, autoInit bool) (*FS, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, systemfs.NewInvalidPathError(err)
	}

	// Automatically add execute permissions if read bits are present but exec bits are missing.
	if mode&0444 == 0444 && mode&0111 == 0 {
		mode |= 0111 // e.g. 0444 becomes 0555; 0644 becomes 0755.
	}

	if autoInit {
		if err := os.MkdirAll(dirPath, mode); err != nil {
			return nil, systemfs.NewCreateDirectoryError(err)
		}
	}

	root, err := os.OpenRoot(absPath)
	if err != nil {
		return nil, systemfs.NewOpenDirectoryError(err)
	}

	return &FS{
		root:    root,
		dirPath: absPath,
		mode:    mode,
	}, nil
}

// normalizePath maps an absolute path to a relative one.
// If the user passes "/" (or a path starting with "/"), we strip the leading slash.
// In particular, "/" becomes "." so that it refers to the FS's root.
func (d *FS) normalizePath(name string) string {
	if name == "/" {
		return "."
	}
	if len(name) > 0 && name[0] == '/' {
		return name[1:]
	}
	return name
}

// checkPermissions centralizes permission checking logic.
// It checks only the owner's permission bits and provides debug details.
func (d *FS) checkPermissions(op, displayPath string, check permCheck) error {
	if d.closed.Load() {
		return &fs.PathError{
			Op:   op,
			Path: displayPath,
			Err:  fsapi.ErrClosed,
		}
	}

	var required fs.FileMode
	if check&permRead != 0 {
		required |= 0400
	}
	if check&permWrite != 0 {
		required |= 0200
	}
	if check&permExec != 0 {
		required |= 0100
	}

	ownerMode := d.mode & 0700
	if ownerMode&required != required {
		return &fs.PathError{
			Op:   op,
			Path: displayPath,
			Err:  systemfs.NewPermissionDeniedError(required, ownerMode, fsapi.ErrPermissionDenied),
		}
	}
	return nil
}

// Open implements fs.FS.
func (d *FS) Open(name string) (fs.File, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("open", displayName, permRead); err != nil {
		return nil, err
	}

	if info, err := d.root.Stat(norm); err == nil && info.IsDir() {
		if err := d.checkPermissions("open", displayName, permExec); err != nil {
			return nil, err
		}
	}

	f, err := d.root.Open(norm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: displayName,
			Err:  err,
		}
	}

	return f, nil
}

// OpenFile implements WriteFS.
func (d *FS) OpenFile(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	displayName := name
	norm := d.normalizePath(name)

	// Check if the provided perm has bits outside of fs.ModePerm.
	if perm&^fs.ModePerm != 0 {
		return nil, &fs.PathError{
			Op:   "open",
			Path: displayName,
			Err:  fsapi.ErrInvalidFileMode,
		}
	}

	if d.closed.Load() {
		return nil, &fs.PathError{
			Op:   "open",
			Path: displayName,
			Err:  fsapi.ErrClosed,
		}
	}

	// Check permissions based on flags.
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		if err := d.checkPermissions("open", displayName, permWrite); err != nil {
			return nil, err
		}
	}
	if flag&os.O_RDWR != 0 {
		if err := d.checkPermissions("open", displayName, permRead); err != nil {
			return nil, err
		}
	}

	// Restrict permissions to the FS's mode.
	perm &= d.mode

	f, err := d.root.OpenFile(norm, flag, perm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: displayName,
			Err:  err,
		}
	}

	return f, nil
}

// ReadDir implements fs.ReadDirFS.
func (d *FS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	displayName := name
	norm := d.normalizePath(name)

	if err = d.checkPermissions("readdir", displayName, permRead|permExec); err != nil {
		return nil, err
	}

	f, err := d.root.Open(norm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "readdir",
			Path: displayName,
			Err:  err,
		}
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	return f.ReadDir(-1)
}

// Stat implements fs.StatFS.
func (d *FS) Stat(name string) (fs.FileInfo, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("stat", displayName, permRead); err != nil {
		return nil, err
	}

	info, err := d.root.Stat(norm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "stat",
			Path: displayName,
			Err:  err,
		}
	}

	return info, nil
}

// Remove implements WriteFS.
func (d *FS) Remove(name string) error {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("remove", displayName, permWrite); err != nil {
		return err
	}

	if err := d.root.Remove(norm); err != nil {
		return &fs.PathError{
			Op:   "remove",
			Path: displayName,
			Err:  err,
		}
	}

	return nil
}

// Mkdir implements WriteFS.
func (d *FS) Mkdir(name string, perm fs.FileMode) error {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("mkdir", displayName, permWrite); err != nil {
		return err
	}
	if err := d.checkPermissions("mkdir", displayName, permExec); err != nil {
		return err
	}

	perm &= d.mode

	if err := d.root.Mkdir(norm, perm); err != nil {
		return &fs.PathError{
			Op:   "mkdir",
			Path: displayName,
			Err:  err,
		}
	}

	return nil
}

// Lstat implements fsapi.ReadFS.Lstat.
func (d *FS) Lstat(name string) (fs.FileInfo, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("lstat", displayName, permRead); err != nil {
		return nil, err
	}

	info, err := d.root.Lstat(norm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "lstat",
			Path: displayName,
			Err:  err,
		}
	}

	return info, nil
}

// Rename implements fsapi.WriteFS.Rename.
func (d *FS) Rename(oldname, newname string) error {
	displayOld := oldname
	normOld := d.normalizePath(oldname)
	normNew := d.normalizePath(newname)

	if err := d.checkPermissions("rename", displayOld, permWrite); err != nil {
		return err
	}

	if err := d.root.Rename(normOld, normNew); err != nil {
		return &fs.PathError{
			Op:   "rename",
			Path: displayOld,
			Err:  err,
		}
	}

	return nil
}

// Truncate implements WriteFS.Truncate.
func (d *FS) Truncate(name string, size int64) error {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("truncate", displayName, permWrite); err != nil {
		return err
	}

	f, err := d.root.OpenFile(norm, os.O_WRONLY, 0)
	if err != nil {
		return &fs.PathError{
			Op:   "truncate",
			Path: displayName,
			Err:  err,
		}
	}
	defer func() { _ = f.Close() }()

	if err := f.Truncate(size); err != nil {
		return &fs.PathError{
			Op:   "truncate",
			Path: displayName,
			Err:  err,
		}
	}

	return nil
}

// Chtimes implements WriteFS.Chtimes.
func (d *FS) Chtimes(name string, atime, mtime time.Time) error {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("chtimes", displayName, permWrite); err != nil {
		return err
	}

	if err := d.root.Chtimes(norm, atime, mtime); err != nil {
		return &fs.PathError{
			Op:   "chtimes",
			Path: displayName,
			Err:  err,
		}
	}

	return nil
}

// Close releases resources.
func (d *FS) Close() error {
	if d.closed.CompareAndSwap(false, true) {
		return d.root.Close()
	}
	return nil
}
