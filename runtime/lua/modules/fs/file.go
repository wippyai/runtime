package fs

import (
	"errors"
	"fmt"
	fsapi "github.com/ponyruntime/pony/api/fs"
	lua "github.com/yuin/gopher-lua"
	"io"
	"io/fs"
	"sync"
)

type File struct {
	file fsapi.File
	once sync.Once
}

// Read implements io.Reader.
func (f *File) Read(p []byte) (int, error) {
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
	pos, err := f.file.Seek(offset, whence)
	if err != nil {
		return pos, fmt.Errorf("failed to seek: %w", err)
	}
	return pos, nil
}

// Stat implements fs.File.
func (f *File) Stat() (fs.FileInfo, error) {
	info, err := f.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	return info, nil
}

func (f *File) Sync() error {
	// Check if underlying file implements Sync
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}
	return nil
}

func registerFile(l *lua.LState) {
	mt := l.NewTypeMetatable("fs.File")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"read":  fileRead,
		"write": fileWrite,
		"seek":  fileSeek,
		"close": fileClose,
		"stat":  fileStat,
		"sync":  fileSync,
	}))
}

// Close implements io.Closer.
func (f *File) Close() error {
	var err error
	f.once.Do(func() {
		err = f.file.Close()
	})

	// Don't return an error for already closed files
	// This is the key fix - if we get ErrClosed, we shouldn't treat it as an error
	if err != nil && errors.Is(err, fs.ErrClosed) {
		return nil
	}

	return err
}

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
		// Don't treat "already closed" as an error
		l.RaiseError("close error: %s", err)
		return 0
	}

	return 0
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
