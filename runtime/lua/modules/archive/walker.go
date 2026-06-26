// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	lua "github.com/wippyai/go-lua"
	archiveapi "github.com/wippyai/runtime/api/archive"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
	"github.com/wippyai/runtime/runtime/security"
)

type luaWalker struct {
	w             archiveapi.Walker
	cancelCleanup func()
	mu            sync.Mutex
	closed        bool
}

func newLuaWalker(ctx context.Context, w archiveapi.Walker) *luaWalker {
	lw := &luaWalker{w: w}
	if store := resource.GetStore(ctx); store != nil {
		lw.cancelCleanup = store.AddCleanup(lw.closeOnce)
	}
	return lw
}

func (lw *luaWalker) closeOnce() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.closed {
		return nil
	}
	lw.closed = true
	return lw.w.Close()
}

// streamReader pulls an io.Reader out of a Lua source value (a stream.Stream,
// an fs.File, or raw bytes).
func streamReader(l *lua.LState, idx int) (io.Reader, bool) {
	switch v := l.Get(idx).(type) {
	case lua.LString:
		return bytes.NewReader([]byte(string(v))), true
	case *lua.LUserData:
		if rp, ok := v.Value.(resource.ReaderProvider); ok {
			r, err := rp.GetReader(l.Context())
			if err == nil {
				return r, true
			}
		}
		if f, ok := v.Value.(*fsmod.File); ok {
			return f.Backend(), true
		}
		if r, ok := v.Value.(io.Reader); ok {
			return r, true
		}
	}
	return nil, false
}

func archiveScan(l *lua.LState) int {
	ctx := l.Context()
	if !security.IsAllowed(ctx, "archive.read", "", nil) {
		return permissionError(l, "not allowed to read archives")
	}
	r, ok := streamReader(l, 1)
	if !ok {
		return invalidError(l, "source must be a stream, an fs file, or bytes")
	}
	o := parseOptions(l, 2)

	br := bufio.NewReaderSize(r, 64<<10)
	header, _ := br.Peek(512)
	c, found := archiveapi.Resolve(o.Format, "", header)
	if !found {
		return invalidError(l, "unknown archive format (set opts.format)")
	}
	sc, ok := c.(archiveapi.StreamReadable)
	if !ok {
		return unavailableError(l, "format "+c.Name()+" has no streaming reader")
	}
	w, err := sc.OpenStream(br, o)
	if err != nil {
		return internalError(l, err, "open archive stream")
	}
	value.PushUserData(l, newLuaWalker(ctx, w), walkerMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkWalker(l *lua.LState, idx int) *luaWalker {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*luaWalker); ok {
		return v
	}
	l.ArgError(idx, "archive.Walker expected")
	return nil
}

func walkerToString(l *lua.LState) int {
	l.Push(lua.LString("archive.Walker{}"))
	return 1
}

var walkerMethods = map[string]lua.LGoFunc{
	"walk":        walkerWalk,
	"extract_all": walkerExtractAll,
	"close":       walkerClose,
}

type readNopCloser struct{ r io.Reader }

func (rn readNopCloser) Read(p []byte) (int, error) { return rn.r.Read(p) }
func (readNopCloser) Close() error                  { return nil }

func walkerWalk(l *lua.LState) int {
	lw := checkWalker(l, 1)
	if lw == nil {
		return 0
	}
	l.Push(lua.LGoFunc(walkerWalkNext))
	ud := l.NewUserData()
	ud.Value = lw
	l.Push(ud)
	return 2
}

func walkerWalkNext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	lw, ok := ud.Value.(*luaWalker)
	if !ok {
		return 0
	}
	e, r, err := lw.w.Next()
	if errors.Is(err, io.EOF) {
		l.Push(lua.LNil)
		return 1
	}
	if err != nil {
		l.RaiseError("archive walk: %v", err)
		return 0
	}
	table := resource.GetTable(l.Context())
	l.Push(entryTable(l, e))
	if table != nil && !e.IsDir {
		id := streamsys.InsertWithSize(table, readNopCloser{r}, e.Size)
		l.Push(streammod.NewStream(l, id))
	} else {
		l.Push(lua.LNil)
	}
	return 2
}

func walkerExtractAll(l *lua.LState) int {
	lw := checkWalker(l, 1)
	if lw == nil {
		return 0
	}
	dest, ok := checkFSArg(l, 2)
	if !ok {
		return invalidError(l, "destination must be an fs handle")
	}
	prefix, strip, filterFn := extractOptions(l, 3)
	count := 0
	for {
		e, r, err := lw.w.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return internalError(l, err, "walk")
		}
		name := applyStrip(e.Name, strip)
		if name == "" {
			continue
		}
		if filterFn != nil && !runFilter(l, filterFn, e) {
			continue
		}
		clean, ok := sysarchiveSanitize(prefix + name)
		if !ok {
			continue
		}
		if e.IsDir {
			mkdirAll(dest, strings.TrimSuffix(clean, "/"))
			continue
		}
		if err := writeToFS(dest, clean, readNopCloser{r}, 64<<10); err != nil {
			return internalError(l, err, "extract "+e.Name)
		}
		count++
	}
	l.Push(lua.LNumber(count))
	l.Push(lua.LNil)
	return 2
}

func walkerClose(l *lua.LState) int {
	lw := checkWalker(l, 1)
	if lw == nil {
		return 0
	}
	if lw.cancelCleanup != nil {
		lw.cancelCleanup()
		lw.cancelCleanup = nil
	}
	if err := lw.closeOnce(); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "close").WithKind(lua.Internal))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
