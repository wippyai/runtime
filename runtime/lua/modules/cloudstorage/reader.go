// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"strings"
	"sync"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const readerTypeName = "cloudstorage.Reader"

const (
	minReaderBlockSize = 64 * 1024
	maxReaderBlockSize = csapi.MaxReaderBlockSize
)

type luaRangeReader struct {
	r             *csapi.RangeReaderAt
	cancelCleanup func()
	name          string
	mu            sync.Mutex
	closed        bool
}

func newLuaRangeReader(ctx context.Context, r *csapi.RangeReaderAt, name string) *luaRangeReader {
	lr := &luaRangeReader{r: r, name: name}
	if store := resource.GetStore(ctx); store != nil {
		lr.cancelCleanup = store.AddCleanup(lr.closeOnce)
	}
	return lr
}

func (lr *luaRangeReader) ReadAt(p []byte, off int64) (int, error) { return lr.r.ReadAt(p, off) }

func (lr *luaRangeReader) Size() int64 { return lr.r.Size() }

func (lr *luaRangeReader) Name() string { return lr.name }

func (lr *luaRangeReader) closeOnce() error {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.closed {
		return nil
	}
	lr.closed = true
	return lr.r.Close()
}

var readerMethods = map[string]lua.LGoFunc{
	"size":  readerSize,
	"key":   readerKey,
	"close": readerClose,
}

var readerMetamethods = map[string]lua.LGoFunc{
	"__tostring": readerToString,
}

func checkReader(l *lua.LState, idx int) *luaRangeReader {
	ud := l.CheckUserData(idx)
	if lr, ok := ud.Value.(*luaRangeReader); ok {
		return lr
	}
	l.ArgError(idx, "cloudstorage.Reader expected")
	return nil
}

func readerToString(l *lua.LState) int {
	l.Push(lua.LString(readerTypeName))
	return 1
}

func readerSize(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	l.Push(lua.LNumber(lr.Size()))
	return 1
}

func readerKey(l *lua.LState) int {
	lr := checkReader(l, 1)
	if lr == nil {
		return 0
	}
	l.Push(lua.LString(lr.r.Key()))
	return 1
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

func newRangeReaderValue(l *lua.LState, storage csapi.Storage, key string, size int64, etag string, blockSize int64, cacheBlocks int) *lua.LUserData {
	opts := &csapi.RangeReaderAtOptions{
		BlockSize:   blockSize,
		CacheBlocks: cacheBlocks,
		ETag:        etag,
	}
	ra := csapi.NewRangeReaderAt(l.Context(), storage, key, size, opts)

	name := key
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		name = key[i+1:]
	}

	lr := newLuaRangeReader(l.Context(), ra, name)
	return value.NewTypedUserData(l, lr, readerTypeName)
}
