// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

	lua "github.com/wippyai/go-lua"
	archiveapi "github.com/wippyai/runtime/api/archive"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/runtime/runtime/security"
	streamsys "github.com/wippyai/runtime/system/stream"
)

type luaWriter struct {
	w             archiveapi.Writer
	closer        io.Closer
	cancelCleanup func()
	format        string
	bufSize       int
	mu            sync.Mutex
	closed        bool
}

func newLuaWriter(ctx context.Context, w archiveapi.Writer, closer io.Closer, format string, bufSize int) *luaWriter {
	lw := &luaWriter{w: w, closer: closer, format: format, bufSize: bufSize}
	if store := resource.GetStore(ctx); store != nil {
		lw.cancelCleanup = store.AddCleanup(func() error {
			lw.mu.Lock()
			defer lw.mu.Unlock()
			if lw.closed {
				return nil
			}
			lw.closed = true
			return lw.finalize()
		})
	}
	return lw
}

func (lw *luaWriter) finalize() error {
	err := lw.w.Close()
	if lw.closer != nil {
		if cerr := lw.closer.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

func (lw *luaWriter) isTar() bool { return strings.HasPrefix(lw.format, "tar") }

// writeDest resolves the create() destination into an io.Writer plus a closer
// that owns the underlying sink (nil when the caller owns it, e.g. a stream).
func writeDest(l *lua.LState) (w io.Writer, closer io.Closer, name string, optsIdx int, errCode int) {
	ud, ok := l.Get(1).(*lua.LUserData)
	if !ok {
		return nil, nil, "", 0, invalidError(l, "destination must be an fs handle, an fs file, or a writable stream")
	}
	switch h := ud.Value.(type) {
	case *fsmod.FS:
		path := l.CheckString(2)
		resolved, err := h.Resolve(path)
		if err != nil {
			return nil, nil, "", 0, internalError(l, err, "resolve path")
		}
		f, err := h.Backend().OpenFile(resolved, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, nil, "", 0, internalError(l, err, "open destination")
		}
		return f, f, path, 3, 0
	case *fsmod.File:
		return h.Backend(), nil, "", 2, 0
	case *streammod.Stream:
		table := resource.GetTable(l.Context())
		if table == nil {
			return nil, nil, "", 0, internalError(l, errors.New("no resource table"), "create")
		}
		entry, err := streamsys.Get(table, h.ID)
		if err != nil {
			return nil, nil, "", 0, internalError(l, err, "resolve stream")
		}
		if !entry.Caps().Writable {
			return nil, nil, "", 0, invalidError(l, "destination stream is not writable")
		}
		return entry.Writer(), nil, "", 2, 0
	}
	return nil, nil, "", 0, invalidError(l, "destination must be an fs handle, an fs file, or a writable stream")
}

func archiveCreate(l *lua.LState) int {
	ctx := l.Context()
	if !security.IsAllowed(ctx, "archive.write", "", nil) {
		return permissionError(l, "not allowed to write archives")
	}
	dst, closer, name, optsIdx, code := writeDest(l)
	if code != 0 {
		return code
	}
	o := parseOptions(l, optsIdx)
	c, found := archiveapi.Resolve(o.Format, name, nil)
	if !found {
		if closer != nil {
			_ = closer.Close()
		}
		return invalidError(l, "unknown archive format (set opts.format)")
	}
	wc, ok := c.(archiveapi.Writable)
	if !ok {
		if closer != nil {
			_ = closer.Close()
		}
		return unavailableError(l, "format "+c.Name()+" is not writable")
	}
	w, err := wc.OpenWriter(dst, o)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return internalError(l, err, "create archive")
	}
	value.PushUserData(l, newLuaWriter(ctx, w, closer, c.Name(), bufferSize(o)), writerMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkWriter(l *lua.LState, idx int) *luaWriter {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*luaWriter); ok {
		return v
	}
	l.ArgError(idx, "archive.Writer expected")
	return nil
}

func writerToString(l *lua.LState) int {
	l.Push(lua.LString("archive.Writer{}"))
	return 1
}

var writerMethods = map[string]lua.LGoFunc{
	"add":      writerAdd,
	"add_file": writerAddFile,
	"add_dir":  writerAddDir,
	"close":    writerClose,
}

func entryOptions(l *lua.LState, idx int) (method string, mode fs.FileMode, size int64, hasSize bool) {
	size = -1
	if idx > l.GetTop() || l.Get(idx).Type() != lua.LTTable {
		return "", 0, size, false
	}
	t := l.ToTable(idx)
	if v := t.RawGetString("method"); v.Type() == lua.LTString {
		method = v.String()
	}
	if v := t.RawGetString("mode"); v.Type() == lua.LTNumber || v.Type() == lua.LTInteger {
		mode = fs.FileMode(uint32(lua.LVAsNumber(v)))
	}
	if v := t.RawGetString("size"); v.Type() == lua.LTNumber || v.Type() == lua.LTInteger {
		size = int64(lua.LVAsNumber(v))
		hasSize = true
	}
	return method, mode, size, hasSize
}

func (lw *luaWriter) addEntry(l *lua.LState, name string, r io.Reader, size int64, method string, mode fs.FileMode) int {
	if lw.isTar() && size < 0 {
		return invalidError(l, "size required to stream an entry into a tar archive (pass opts.size)")
	}
	e := archiveapi.Entry{Name: name, Size: size, Method: method, Mode: mode}
	ew, err := lw.w.Create(e)
	if err != nil {
		return internalError(l, err, "create entry")
	}
	buf := make([]byte, lw.bufSize)
	if _, err := io.CopyBuffer(ew, r, buf); err != nil {
		return internalError(l, err, "write entry")
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func writerAdd(l *lua.LState) int {
	lw := checkWriter(l, 1)
	if lw == nil {
		return 0
	}
	name := l.CheckString(2)
	method, mode, optSize, hasSize := entryOptions(l, 4)

	switch v := l.Get(3).(type) {
	case lua.LString:
		data := []byte(string(v))
		return lw.addEntry(l, name, strings.NewReader(string(data)), int64(len(data)), method, mode)
	case *lua.LUserData:
		r, ok := streamReader(l, 3)
		if !ok {
			return invalidError(l, "data must be a string, a stream, or an fs file")
		}
		size := int64(-1)
		if hasSize {
			size = optSize
		}
		_ = v
		return lw.addEntry(l, name, r, size, method, mode)
	default:
		return invalidError(l, "data must be a string, a stream, or an fs file")
	}
}

func writerAddFile(l *lua.LState) int {
	lw := checkWriter(l, 1)
	if lw == nil {
		return 0
	}
	name := l.CheckString(2)
	src, ok := checkFSArg(l, 3)
	if !ok {
		return invalidError(l, "source must be an fs handle")
	}
	srcPath := l.CheckString(4)
	method, mode, _, _ := entryOptions(l, 5)

	f, err := src.OpenFile(srcPath, os.O_RDONLY, 0)
	if err != nil {
		return internalError(l, err, "open source file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return internalError(l, err, "stat source file")
	}
	if mode == 0 {
		mode = info.Mode()
	}
	return lw.addEntry(l, name, f, info.Size(), method, mode)
}

func writerAddDir(l *lua.LState) int {
	lw := checkWriter(l, 1)
	if lw == nil {
		return 0
	}
	name := l.CheckString(2)
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}
	if _, err := lw.w.Create(archiveapi.Entry{Name: name, IsDir: true, Mode: fs.ModeDir | 0o755}); err != nil {
		return internalError(l, err, "create directory entry")
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func writerClose(l *lua.LState) int {
	lw := checkWriter(l, 1)
	if lw == nil {
		return 0
	}
	if lw.cancelCleanup != nil {
		lw.cancelCleanup()
		lw.cancelCleanup = nil
	}
	lw.mu.Lock()
	if lw.closed {
		lw.mu.Unlock()
		l.Push(lua.LTrue)
		l.Push(lua.LNil)
		return 2
	}
	lw.closed = true
	err := lw.finalize()
	lw.mu.Unlock()
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "close").WithKind(lua.Internal))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
