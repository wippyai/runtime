// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	lua "github.com/wippyai/go-lua"
	archiveapi "github.com/wippyai/runtime/api/archive"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
)

func newBytesReaderAt(b []byte) *bytes.Reader { return bytes.NewReader(b) }

type luaReader struct {
	r             archiveapi.Reader
	closer        io.Closer
	cancelCleanup func()
	opts          archiveapi.Options
	mu            sync.Mutex
	closed        bool
}

func newLuaReader(ctx context.Context, r archiveapi.Reader, closer io.Closer, o archiveapi.Options) *luaReader {
	lr := &luaReader{r: r, closer: closer, opts: o}
	if store := resource.GetStore(ctx); store != nil {
		lr.cancelCleanup = store.AddCleanup(lr.closeOnce)
	}
	return lr
}

func (lr *luaReader) closeOnce() error {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.closed {
		return nil
	}
	lr.closed = true
	err := lr.r.Close()
	if lr.closer != nil {
		if cerr := lr.closer.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

func pushReader(l *lua.LState, lr *luaReader) {
	value.PushUserData(l, lr, readerMetatable)
}

func checkReader(l *lua.LState, idx int) *luaReader {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*luaReader); ok {
		return v
	}
	l.ArgError(idx, "archive.Reader expected")
	return nil
}

func readerToString(l *lua.LState) int {
	l.Push(lua.LString("archive.Reader{}"))
	return 1
}

var readerMethods = map[string]lua.LGoFunc{
	"entries":     readerEntries,
	"stat":        readerStat,
	"read":        readerRead,
	"stream":      readerStream,
	"extract":     readerExtract,
	"extract_all": readerExtractAll,
	"close":       readerClose,
}

func entryTable(l *lua.LState, e archiveapi.Entry) *lua.LTable {
	t := l.CreateTable(0, 9)
	t.RawSetString("name", lua.LString(e.Name))
	t.RawSetString("size", lua.LNumber(e.Size))
	t.RawSetString("compressed_size", lua.LNumber(e.CompressedSize))
	t.RawSetString("is_dir", lua.LBool(e.IsDir))
	t.RawSetString("mode", lua.LNumber(uint32(e.Mode)))
	t.RawSetString("modified", lua.LNumber(e.Modified.Unix()))
	t.RawSetString("method", lua.LString(e.Method))
	t.RawSetString("crc32", lua.LNumber(e.CRC32))
	if e.IsDir {
		t.RawSetString("type", lua.LString("directory"))
	} else {
		t.RawSetString("type", lua.LString("file"))
	}
	return t
}

type entriesIter struct {
	entries []archiveapi.Entry
	i       int
}

func readerEntries(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	st := &entriesIter{entries: lr.r.Entries()}
	ud := l.NewUserData()
	ud.Value = st
	l.Push(lua.LGoFunc(entriesIterNext))
	l.Push(ud)
	return 2
}

func entriesIterNext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	st, ok := ud.Value.(*entriesIter)
	if !ok {
		return 0
	}
	if st.i >= len(st.entries) {
		l.Push(lua.LNil)
		return 1
	}
	e := st.entries[st.i]
	st.i++
	l.Push(entryTable(l, e))
	return 1
}

func readerStat(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	name := l.CheckString(2)
	e, ok := lr.r.Stat(name)
	if !ok {
		return notFoundError(l, "entry not found: "+name)
	}
	l.Push(entryTable(l, e))
	l.Push(lua.LNil)
	return 2
}

func readerRead(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	name := l.CheckString(2)
	e, ok := lr.r.Stat(name)
	if !ok {
		return notFoundError(l, "entry not found: "+name)
	}
	maxInline := lr.opts.MaxInlineBytes
	if maxInline == 0 {
		maxInline = sysarchiveInline()
	}
	if e.Size > maxInline {
		return invalidError(l, "entry too large for read(); use stream() or extract()")
	}
	rc, _, err := lr.r.Open(name)
	if err != nil {
		return internalError(l, err, "open entry")
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxInline+1))
	if err != nil {
		return internalError(l, err, "read entry")
	}
	if int64(len(data)) > maxInline {
		return invalidError(l, "entry exceeds max_inline_bytes")
	}
	l.Push(lua.LString(data))
	l.Push(lua.LNil)
	return 2
}

func readerStream(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	name := l.CheckString(2)
	rc, e, err := lr.r.Open(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return notFoundError(l, "entry not found: "+name)
		}
		return internalError(l, err, "open entry")
	}
	table := resource.GetTable(l.Context())
	if table == nil {
		_ = rc.Close()
		return internalError(l, errors.New("no resource table"), "stream entry")
	}
	id := streamsys.InsertWithSize(table, rc, e.Size)
	l.Push(streammod.NewStream(l, id))
	l.Push(lua.LNil)
	return 2
}

func checkFSArg(l *lua.LState, idx int) (fsapi.FS, bool) {
	ud, ok := l.Get(idx).(*lua.LUserData)
	if !ok {
		return nil, false
	}
	h, ok := ud.Value.(*fsmod.FS)
	if !ok {
		return nil, false
	}
	return h.Backend(), true
}

func readerExtract(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	name := l.CheckString(2)
	dest, ok := checkFSArg(l, 3)
	if !ok {
		return invalidError(l, "destination must be an fs handle")
	}
	destPath := name
	if l.GetTop() >= 4 && l.Get(4).Type() == lua.LTString {
		destPath = l.CheckString(4)
	}
	clean, ok := archiveSanitize(destPath)
	if !ok {
		return invalidError(l, "unsafe entry path: "+destPath)
	}
	rc, _, err := lr.r.Open(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return notFoundError(l, "entry not found: "+name)
		}
		return internalError(l, err, "open entry")
	}
	if _, err := writeToFS(dest, clean, rc, bufferSize(lr.opts)); err != nil {
		return internalError(l, err, "extract entry")
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func readerExtractAll(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	dest, ok := checkFSArg(l, 2)
	if !ok {
		return invalidError(l, "destination must be an fs handle")
	}
	prefix, strip, filterFn := extractOptions(l, 3)
	maxTotal := maxTotalBytes(lr.opts)
	var total int64
	count := 0
	for _, e := range lr.r.Entries() {
		name := applyStrip(e.Name, strip)
		if name == "" {
			continue
		}
		if filterFn != nil && !runFilter(l, filterFn, e) {
			continue
		}
		clean, ok := archiveSanitize(prefix + name)
		if !ok {
			continue
		}
		if e.IsDir {
			mkdirAll(dest, strings.TrimSuffix(clean, "/"))
			continue
		}
		rc, _, err := lr.r.Open(e.Name)
		if err != nil {
			return internalError(l, err, "open entry "+e.Name)
		}
		n, err := writeToFS(dest, clean, rc, bufferSize(lr.opts))
		if err != nil {
			return internalError(l, err, "extract "+e.Name)
		}
		total += n
		if total > maxTotal {
			return invalidError(l, "archive exceeds max_total_bytes")
		}
		count++
	}
	l.Push(lua.LNumber(count))
	l.Push(lua.LNil)
	return 2
}

func readerClose(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	if lr.cancelCleanup != nil {
		lr.cancelCleanup()
		lr.cancelCleanup = nil
	}
	if err := lr.closeOnce(); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "close").WithKind(lua.Internal))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// --- shared extraction helpers ---

func writeToFS(dest fsapi.FS, destPath string, rc io.ReadCloser, bufSize int) (int64, error) {
	defer rc.Close()
	if dir := path.Dir(destPath); dir != "." && dir != "/" {
		mkdirAll(dest, dir)
	}
	f, err := dest.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, bufSize)
	n, copyErr := io.CopyBuffer(f, rc, buf)
	closeErr := f.Close()
	if copyErr != nil {
		return n, copyErr
	}
	return n, closeErr
}

func mkdirAll(dest fsapi.FS, dir string) {
	cur := ""
	for _, p := range strings.Split(dir, "/") {
		if p == "" {
			continue
		}
		if cur == "" {
			cur = p
		} else {
			cur += "/" + p
		}
		_ = dest.Mkdir(cur, 0o755)
	}
}

func applyStrip(name string, strip int) string {
	if strip <= 0 {
		return name
	}
	parts := strings.Split(name, "/")
	if len(parts) <= strip {
		return ""
	}
	return strings.Join(parts[strip:], "/")
}

func extractOptions(l *lua.LState, idx int) (prefix string, strip int, filter lua.LValue) {
	if idx > l.GetTop() || l.Get(idx).Type() != lua.LTTable {
		return "", 0, nil
	}
	t := l.ToTable(idx)
	if v := t.RawGetString("prefix"); v.Type() == lua.LTString {
		prefix = v.String()
	}
	if v := t.RawGetString("strip"); v.Type() == lua.LTNumber || v.Type() == lua.LTInteger {
		strip = int(lua.LVAsNumber(v))
	}
	if v := t.RawGetString("filter"); v.Type() == lua.LTFunction {
		filter = v
	}
	return prefix, strip, filter
}

func runFilter(l *lua.LState, fn lua.LValue, e archiveapi.Entry) bool {
	l.Push(fn)
	l.Push(entryTable(l, e))
	if err := l.PCall(1, 1, nil); err != nil {
		return false
	}
	ret := l.Get(-1)
	l.Pop(1)
	return lua.LVAsBool(ret)
}

func archiveSanitize(name string) (string, bool) {
	return sysarchiveSanitize(name)
}
