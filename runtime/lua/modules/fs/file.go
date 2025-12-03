package fs

import (
	"context"
	"errors"
	"io"
	basefs "io/fs"
	"sync"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/resource"
	lua "github.com/yuin/gopher-lua"
)

type File struct {
	file          fsapi.File
	closed        bool
	mu            sync.Mutex
	cancelCleanup func()
}

func NewFile(file fsapi.File) *File {
	return &File{file: file}
}

func NewFileWithCleanup(ctx context.Context, file fsapi.File) *File {
	f := &File{file: file}

	store := resource.GetStore(ctx)
	if store != nil {
		f.cancelCleanup = store.AddCleanup(func() error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.closed {
				f.closed = true
				return f.file.Close()
			}
			return nil
		})
	}

	return f
}

func (f *File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrFileAlreadyClosed
	}

	n, err := f.file.Read(p)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return n, err
		}
		if errors.Is(err, basefs.ErrClosed) {
			return n, ErrFileAlreadyClosed
		}
		return n, NewReadError(err)
	}
	return n, nil
}

func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrFileAlreadyClosed
	}

	n, err := f.file.Write(p)
	if err != nil {
		return n, NewWriteError(err)
	}
	return n, nil
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrFileAlreadyClosed
	}

	pos, err := f.file.Seek(offset, whence)
	if err != nil {
		return pos, NewSeekError(err)
	}
	return pos, nil
}

func (f *File) Stat() (basefs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, ErrFileAlreadyClosed
	}

	info, err := f.file.Stat()
	if err != nil {
		return nil, NewStatError(err)
	}
	return info, nil
}

func (f *File) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrFileAlreadyClosed
	}

	if err := f.file.Sync(); err != nil {
		return NewSyncError(err)
	}
	return nil
}

func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}

	f.closed = true
	cancel := f.cancelCleanup
	f.cancelCleanup = nil

	if cancel != nil {
		cancel()
	}

	return f.file.Close()
}

var fileMethods = map[string]lua.LGFunction{
	"read":  fileRead,
	"write": fileWrite,
	"seek":  fileSeek,
	"close": fileClose,
	"stat":  fileStat,
	"sync":  fileSync,
}

func fileRead(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	size := l.OptInt(2, 4096)
	if size <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("size must be positive"))
		return 2
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
	l.Push(lua.LNil)
	return 2
}

func fileWrite(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	data := l.CheckString(2)
	if data == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("data required"))
		return 2
	}

	_, err := f.Write([]byte(data))
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fileSeek(l *lua.LState) int {
	f := checkFile(l, 1)
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
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid whence: must be 'set', 'cur', or 'end'"))
		return 2
	}

	pos, err := f.Seek(offset, w)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LNumber(pos))
	l.Push(lua.LNil)
	return 2
}

func fileClose(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	err := f.Close()
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fileStat(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	info, err := f.Stat()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(pushFileInfo(l, info))
	l.Push(lua.LNil)
	return 2
}

func fileSync(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	err := f.Sync()
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fileToString(l *lua.LState) int {
	f := checkFile(l, 1)
	if f == nil {
		return 0
	}
	if f.closed {
		l.Push(lua.LString("fs.File{closed}"))
	} else {
		l.Push(lua.LString("fs.File{}"))
	}
	return 1
}
