// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"os"
	"sync"

	lua "github.com/wippyai/go-lua"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/runtime/resource"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	"github.com/wippyai/wapp"
)

const packageTypeName = "hub.Package"

var packageMetatable *lua.LTable

func init() {
	packageMetatable = value.RegisterTypeMethods(nil, packageTypeName,
		map[string]lua.LGoFunc{
			"__index":    packageIndex,
			"__tostring": packageToString,
		},
		nil)
}

// packageHandle is an owned artifact handle. It keeps the wapp file and reader
// open for the lifetime of the frame, mirroring fs.File cleanup semantics: the
// file must stay open because the resource filesystem reads lazily from the
// reader. Cleanup is registered on the resource store and cancelled on close.
type packageHandle struct {
	file          *os.File
	reader        *wapp.Reader
	cancelCleanup func()
	version       string
	digest        string
	mu            sync.Mutex
	closed        bool
	packed        bool
}

func newPackageHandle(ctx context.Context, file *os.File, reader *wapp.Reader, version, digest string) *packageHandle {
	h := &packageHandle{
		version: version,
		digest:  digest,
		file:    file,
		reader:  reader,
		packed:  true,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		h.cancelCleanup = store.AddCleanup(func() error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.closed {
				h.closed = true
				return h.file.Close()
			}
			return nil
		})
	}

	return h
}

func (h *packageHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	h.closed = true
	cancel := h.cancelCleanup
	h.cancelCleanup = nil

	if cancel != nil {
		cancel()
	}

	return h.file.Close()
}

func (h *packageHandle) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func pushPackageHandle(l *lua.LState, h *packageHandle) {
	value.PushUserData(l, h, packageMetatable)
}

func checkPackage(l *lua.LState, idx int) *packageHandle {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*packageHandle); ok {
		return v
	}
	l.ArgError(idx, "hub package expected")
	return nil
}

func pushPackageClosed(l *lua.LState) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, "package handle is closed").WithKind(lua.Invalid).WithRetryable(false))
	return 2
}

func packageIndex(l *lua.LState) int {
	if checkPackage(l, 1) == nil {
		return 0
	}
	h := l.CheckUserData(1).Value.(*packageHandle)
	key := l.CheckString(2)
	switch key {
	case "version":
		l.Push(lua.LString(h.version))
	case "digest":
		l.Push(lua.LString(h.digest))
	case "packed":
		l.Push(lua.LBool(h.packed))
	case "metadata":
		l.Push(lua.LGoFunc(packageMetadata))
	case "entries":
		l.Push(lua.LGoFunc(packageEntries))
	case "resources":
		l.Push(lua.LGoFunc(packageResources))
	case "fs":
		l.Push(lua.LGoFunc(packageFS))
	case "close":
		l.Push(lua.LGoFunc(packageClose))
	default:
		l.Push(lua.LNil)
	}
	return 1
}

func packageToString(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("hub.Package{version=%s}", h.version)))
	return 1
}

func packageMetadata(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	if h.isClosed() {
		return pushPackageClosed(l)
	}

	meta, err := h.reader.GetMetadata()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "read pack metadata").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	tbl, convErr := luaconv.GoToLua(map[string]any(meta))
	if convErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, convErr, "convert pack metadata").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

func packageEntries(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	if h.isClosed() {
		return pushPackageClosed(l)
	}

	kinds, includeData, optErr := parsePackageEntryOptions(l, 2)
	if optErr != nil {
		return pushError(l, optErr)
	}

	entries, err := h.reader.GetEntries()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "read artifact entries").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	arr, convErr := entriesToArray(l, entries, kinds, includeData)
	if convErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, convErr, "convert artifact entries").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(arr)
	l.Push(lua.LNil)
	return 2
}

func packageResources(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	if h.isClosed() {
		return pushPackageClosed(l)
	}

	arr, err := resourcesToArray(l, h.reader.ListResources())
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "convert artifact resources").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(arr)
	l.Push(lua.LNil)
	return 2
}

func packageFS(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	if h.isClosed() {
		return pushPackageClosed(l)
	}

	resourceID := l.CheckString(2)
	if resourceID == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource id required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	rdfs, err := h.reader.GetFS(parseResourceID(resourceID))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "open resource filesystem").WithKind(lua.NotFound).WithRetryable(false))
		return 2
	}

	fsmod.PushFS(l, fsapi.NewReadOnlyFS(rdfs), ".")
	l.Push(lua.LNil)
	return 2
}

func packageClose(l *lua.LState) int {
	h := checkPackage(l, 1)
	if h == nil {
		return 0
	}
	if err := h.Close(); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "close package").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// parsePackageEntryOptions parses the { kind?, include_data? } table accepted by
// pkg:entries. include_data defaults to true, matching hub.versions.entries.
func parsePackageEntryOptions(l *lua.LState, idx int) (map[string]struct{}, bool, *lua.Error) {
	includeData := true
	if l.GetTop() < idx {
		return nil, includeData, nil
	}
	val := l.Get(idx)
	if val == lua.LNil {
		return nil, includeData, nil
	}
	tbl, ok := val.(*lua.LTable)
	if !ok {
		return nil, includeData, invalidOptionError(l, "options", "table", val)
	}

	kinds, _, err := parseKindFilter(l, tbl)
	if err != nil {
		return nil, includeData, err
	}

	if include, ok, err := tableBool(l, tbl, "include_data"); err != nil {
		return nil, includeData, err
	} else if ok {
		includeData = include
	}

	return kinds, includeData, nil
}

// entriesToArray converts wapp entries to a bare Lua array of
// { id = "ns:name", kind, meta, data }. data is omitted when includeData is
// false and stays raw (GoToLua does not resolve ${env:...}/_env literals).
func entriesToArray(_ *lua.LState, entries []wapp.Entry, kinds map[string]struct{}, includeData bool) (*lua.LTable, error) {
	arr := lua.CreateTable(len(entries), 0)
	count := 0
	for _, entry := range entries {
		if kinds != nil {
			if _, ok := kinds[entry.Kind]; !ok {
				continue
			}
		}

		result := lua.CreateTable(0, 4)
		result.RawSetString("id", lua.LString(entry.ID.String()))
		result.RawSetString("kind", lua.LString(entry.Kind))

		meta, err := luaconv.GoToLua(map[string]any(entry.Meta))
		if err != nil {
			return nil, fmt.Errorf("convert entry %s meta: %w", entry.ID.String(), err)
		}
		result.RawSetString("meta", meta)

		if includeData {
			data, err := luaconv.GoToLua(entry.Data)
			if err != nil {
				return nil, fmt.Errorf("convert entry %s data: %w", entry.ID.String(), err)
			}
			result.RawSetString("data", data)
		}

		count++
		arr.RawSetInt(count, result)
	}
	return arr, nil
}

// resourcesToArray converts resource info to a bare Lua array of
// { id = "ns:name", type, hash, size, file_count, meta }.
func resourcesToArray(_ *lua.LState, resources []wapp.ResourceInfo) (*lua.LTable, error) {
	arr := lua.CreateTable(len(resources), 0)
	for i, info := range resources {
		result := lua.CreateTable(0, 6)
		result.RawSetString("id", lua.LString(info.ID.String()))
		result.RawSetString("type", lua.LString(info.Type))
		result.RawSetString("hash", lua.LString(info.Hash))
		result.RawSetString("size", lua.LNumber(info.Size))
		result.RawSetString("file_count", lua.LNumber(info.FileCount))

		meta, err := luaconv.GoToLua(map[string]any(info.Meta))
		if err != nil {
			return nil, fmt.Errorf("convert resource %s meta: %w", info.ID.String(), err)
		}
		result.RawSetString("meta", meta)

		arr.RawSetInt(i+1, result)
	}
	return arr, nil
}
