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
	ErrClosed           = errors.New("filesystem is closed")
	ErrPermissionDenied = errors.New("permission denied")
)

// permCheck represents a permission check type
type permCheck uint32

const (
	_          permCheck = iota
	checkRead            // Check read permissions (0444)
	checkWrite           // Check write permissions (0222)
	checkExec            // Check execute permissions (0111)
)

// FS implements both ReadFS and WriteFS interfaces
type FS struct {
	root    *os.Root
	dirPath string // original path for error messages
	mode    fs.FileMode
	closed  atomic.Bool
}

func NewDirectoryFS(dirPath string, mode fs.FileMode) (*FS, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid directory path: %w", err)
	}

	// Open the root directory safely
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

// checkPermissions centralizes permission checking logic
func (d *FS) checkPermissions(op, path string, check permCheck) error {
	if d.closed.Load() {
		return ErrClosed
	}

	var required fs.FileMode
	switch check {
	case checkRead:
		required = 0444
	case checkWrite:
		required = 0222
	case checkExec:
		required = 0111
	}

	if d.mode&required == 0 {
		return &fs.PathError{
			Op:   op,
			Path: path,
			Err:  ErrPermissionDenied,
		}
	}

	return nil
}

// Open implements fs.FS
func (d *FS) Open(name string) (fs.File, error) {
	// Check read permissions
	if err := d.checkPermissions("open", name, checkRead); err != nil {
		return nil, err
	}

	// For directories, also check execute permission for traversal
	if info, err := d.root.Stat(name); err == nil && info.IsDir() {
		if err := d.checkPermissions("open", name, checkExec); err != nil {
			return nil, err
		}
	}

	f, err := d.root.Open(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}

	return f, nil
}

// OpenFile implements WriteFS
func (d *FS) OpenFile(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	// Check appropriate permissions based on flags
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		if err := d.checkPermissions("open", name, checkWrite); err != nil {
			return nil, err
		}
	}
	if flag&os.O_RDWR != 0 {
		if err := d.checkPermissions("open", name, checkRead); err != nil {
			return nil, err
		}
	}

	// Restrict permissions to filesystem mode
	perm = perm & d.mode

	// Validate permissions
	if perm&^fs.ModePerm != 0 {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  errors.New("invalid file mode"),
		}
	}

	f, err := d.root.OpenFile(name, flag, perm)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}

	return f, nil
}

// ReadDir implements fs.ReadDirFS
func (d *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Check read and execute permissions for directory
	if err := d.checkPermissions("readdir", name, checkRead); err != nil {
		return nil, err
	}
	if err := d.checkPermissions("readdir", name, checkExec); err != nil {
		return nil, err
	}

	f, err := d.root.Open(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "readdir",
			Path: name,
			Err:  err,
		}
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			// Log error but don't override original error if any
			if err == nil {
				err = cerr
			}
		}
	}()

	return f.ReadDir(-1)
}

// Stat implements fs.StatFS
func (d *FS) Stat(name string) (fs.FileInfo, error) {
	// Check read permissions for stat
	if err := d.checkPermissions("stat", name, checkRead); err != nil {
		return nil, err
	}

	info, err := d.root.Stat(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "stat",
			Path: name,
			Err:  err,
		}
	}

	return info, nil
}

// Remove implements WriteFS
func (d *FS) Remove(name string) error {
	// Check write permissions for removal
	if err := d.checkPermissions("remove", name, checkWrite); err != nil {
		return err
	}

	if err := d.root.Remove(name); err != nil {
		return &fs.PathError{
			Op:   "remove",
			Path: name,
			Err:  err,
		}
	}

	return nil
}

// Mkdir implements WriteFS
func (d *FS) Mkdir(name string, perm fs.FileMode) error {
	// Check write permissions for directory creation
	if err := d.checkPermissions("mkdir", name, checkWrite); err != nil {
		return err
	}

	// Also check execute permission as it's needed for directory access
	if err := d.checkPermissions("mkdir", name, checkExec); err != nil {
		return err
	}

	// Restrict permissions to filesystem mode
	perm = perm & d.mode

	if err := d.root.Mkdir(name, perm); err != nil {
		return &fs.PathError{
			Op:   "mkdir",
			Path: name,
			Err:  err,
		}
	}

	return nil
}

// Close releases resources
func (d *FS) Close() error {
	if d.closed.CompareAndSwap(false, true) {
		return d.root.Close()
	}
	return nil
}
