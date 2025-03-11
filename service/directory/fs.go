package directory

import (
	"errors"
	"fmt"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
)

var (
	// ErrClosed is returned when an operation is attempted on a closed filesystem
	ErrClosed = errors.New("filesystem is closed")
	// ErrPermissionDenied is returned when an operation lacks required permissions
	ErrPermissionDenied = errors.New("permission denied")
)

// Define permission flags as proper bit flags.
type permCheck uint32

const (
	_ permCheck = 1 << iota
	checkRead
	checkWrite
	checkExec
)

// FS implements both ReadFS and WriteFS interfaces.
type FS struct {
	root    *os.Root
	dirPath string // original path for error messages
	mode    fs.FileMode
	closed  atomic.Bool
}

// NewDirectoryFS creates a new FS instance. It automatically adds execute bits
// if the read bits are set but the execute bits are missing.
func NewDirectoryFS(dirPath string, mode fs.FileMode) (*FS, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid directory path: %w", err)
	}

	// Automatically add execute permissions if read bits are present but exec bits are missing.
	if mode&0444 == 0444 && mode&0111 == 0 {
		mode |= 0111 // e.g. 0444 becomes 0555; 0644 becomes 0755.
	}

	root, err := os.OpenRoot(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory: %w", err)
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
			Err:  ErrClosed,
		}
	}

	var required fs.FileMode
	if check&checkRead != 0 {
		required |= 0400
	}
	if check&checkWrite != 0 {
		required |= 0200
	}
	if check&checkExec != 0 {
		required |= 0100
	}

	ownerMode := d.mode & 0700
	if ownerMode&required != required {
		return &fs.PathError{
			Op:   op,
			Path: displayPath,
			Err:  fmt.Errorf("%w: required owner bits %o, but FS has %o", ErrPermissionDenied, required, ownerMode),
		}
	}
	return nil
}

// Open implements fs.FS.
func (d *FS) Open(name string) (fs.File, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("open", displayName, checkRead); err != nil {
		return nil, err
	}

	if info, err := d.root.Stat(norm); err == nil && info.IsDir() {
		if err := d.checkPermissions("open", displayName, checkExec); err != nil {
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
			Err:  errors.New("invalid file mode: contains bits outside of fs.ModePerm"),
		}
	}

	if d.closed.Load() {
		return nil, &fs.PathError{
			Op:   "open",
			Path: displayName,
			Err:  ErrClosed,
		}
	}

	// Check permissions based on flags.
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		if err := d.checkPermissions("open", displayName, checkWrite); err != nil {
			return nil, err
		}
	}
	if flag&os.O_RDWR != 0 {
		if err := d.checkPermissions("open", displayName, checkRead); err != nil {
			return nil, err
		}
	}

	// Restrict permissions to the FS's mode.
	perm = perm & d.mode

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
func (d *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("readdir", displayName, checkRead|checkExec); err != nil {
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
		if cerr := f.Close(); cerr != nil {
			if err == nil {
				err = cerr
			}
		}
	}()

	return f.ReadDir(-1)
}

// Stat implements fs.StatFS.
func (d *FS) Stat(name string) (fs.FileInfo, error) {
	displayName := name
	norm := d.normalizePath(name)

	if err := d.checkPermissions("stat", displayName, checkRead); err != nil {
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

	if err := d.checkPermissions("remove", displayName, checkWrite); err != nil {
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

	if err := d.checkPermissions("mkdir", displayName, checkWrite); err != nil {
		return err
	}
	if err := d.checkPermissions("mkdir", displayName, checkExec); err != nil {
		return err
	}

	perm = perm & d.mode

	if err := d.root.Mkdir(norm, perm); err != nil {
		return &fs.PathError{
			Op:   "mkdir",
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
