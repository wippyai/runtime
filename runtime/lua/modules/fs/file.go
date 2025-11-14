package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

type File struct {
	file    fsapi.File
	release context.CancelFunc
	closed  bool
}

// NewFile creates a new wrapped file with UoW integration
func NewFile(uw engine.UnitOfWork, file fsapi.File) *File {
	wrappedFile := &File{file: file}

	// Register cleanup in UoW
	wrappedFile.release = uw.AddCleanup(func() error {
		// Close the file directly
		err := file.Close()
		if err != nil && errors.Is(err, fs.ErrClosed) {
			// Don't report "already closed" as an error
			return nil
		}
		return err
	})

	return wrappedFile
}

// Read implements io.Reader.
func (f *File) Read(p []byte) (int, error) {
	if f.closed {
		return 0, fmt.Errorf("failed to read: file already closed")
	}

	n, err := f.file.Read(p)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return n, err
		}
		if errors.Is(err, fs.ErrClosed) {
			return n, fmt.Errorf("failed to read: file already closed")
		}
		return n, fmt.Errorf("failed to read: %w", err)
	}
	return n, nil
}

// Write implements io.Writer.
func (f *File) Write(p []byte) (int, error) {
	if f.closed {
		return 0, fmt.Errorf("failed to write: file already closed")
	}

	n, err := f.file.Write(p)
	if err != nil {
		return n, fmt.Errorf("failed to write: %w", err)
	}
	if n < len(p) {
		return n, fmt.Errorf("partial write: wrote %d of %d bytes", n, len(p))
	}
	return n, nil
}

// Seek implements io.Seeker.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, fmt.Errorf("failed to seek: file already closed")
	}

	pos, err := f.file.Seek(offset, whence)
	if err != nil {
		return pos, fmt.Errorf("failed to seek: %w", err)
	}
	return pos, nil
}

// Stat implements fs.File.
func (f *File) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, fmt.Errorf("failed to stat: file already closed")
	}

	info, err := f.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	return info, nil
}

func (f *File) Sync() error {
	if f.closed {
		return fmt.Errorf("failed to sync: file already closed")
	}

	// Check if underlying file implements Sync
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}
	return nil
}

// Close implements io.Closer with UoW integration.
// Calls the release function which will both close the file and remove the cleanup from UoW.
func (f *File) Close() error {
	if f.closed {
		return nil // Already closed, no error
	}

	// Mark as closed first to prevent re-entry
	f.closed = true

	// Execute release function which will both close the file and remove the cleanup from UoW
	if f.release != nil {
		f.release()
		f.release = nil
	}

	return nil
}

// Lua integration

func registerFile(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"read":  fileRead,
		"write": fileWrite,
		"seek":  fileSeek,
		"close": fileClose,
		"stat":  fileStat,
		"sync":  fileSync,
	}

	value.RegisterTypeMethods(l, "fs.File", nil, methods)
}

// Helper function to extract Unit of Work from Lua state
//
//nolint:unused // to be used in tests
func getUnitOfWork(l *lua.LState) engine.UnitOfWork {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work missing from context")
		return nil
	}
	return uw
}

// Lua method implementations

func fileRead(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0 // CheckFile will raise error
	}

	size := l.OptInt(2, 4096)
	if size <= 0 {
		l.ArgError(2, "size must be positive")
		return 0
	}

	buf := make([]byte, size)
	n, err := f.Read(buf)

	if err != nil {
		if errors.Is(err, io.EOF) {
			l.Push(lua.LNil)
			l.Push(lua.LString("EOF"))
			return 2
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf[:n]))
	return 1
}

func fileWrite(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0
	}

	data := l.CheckString(2)
	if data == "" {
		l.ArgError(2, "data required")
		return 0
	}

	_, err := f.Write([]byte(data))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

func fileSeek(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0
	}

	whence := l.CheckString(2)
	offset := l.CheckInt64(3)

	var w int
	switch whence {
	case seekSet:
		w = io.SeekStart
	case seekCur:
		w = io.SeekCurrent
	case seekEnd:
		w = io.SeekEnd
	default:
		l.ArgError(2, "invalid whence: must be 'set', 'cur', or 'end'")
		return 0
	}

	pos, err := f.Seek(offset, w)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LNumber(pos))
	return 1
}

func fileClose(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0
	}

	err := f.Close()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Success: the file was either successfully closed or was already closed
	l.Push(lua.LBool(true))
	return 1
}

func fileStat(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0
	}

	info, err := f.Stat()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			l.RaiseError("file does not exist")
			return 0
		}
		l.RaiseError("%s", err.Error())
		return 0
	}

	l.Push(pushFileInfo(l, info))
	return 1
}

func fileSync(l *lua.LState) int {
	f := CheckFile(l, 1)
	if f == nil {
		return 0
	}

	err := f.Sync()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}
