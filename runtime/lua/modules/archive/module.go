// SPDX-License-Identifier: MPL-2.0

// Package archive exposes zip/tar archive reading and writing to Lua with
// bounded memory: archives are never loaded into RAM nor extracted to disk.
package archive

import (
	"io"
	"os"

	lua "github.com/wippyai/go-lua"
	archiveapi "github.com/wippyai/runtime/api/archive"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	sysarchive "github.com/wippyai/runtime/system/archive"

	"github.com/wippyai/runtime/runtime/lua/engine/value"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	"github.com/wippyai/runtime/runtime/security"
)

const (
	readerTypeName = "archive.Reader"
	walkerTypeName = "archive.Walker"
	writerTypeName = "archive.Writer"
)

var (
	readerMetatable *lua.LTable
	walkerMetatable *lua.LTable
	writerMetatable *lua.LTable
)

func init() {
	readerMetatable = value.RegisterTypeMethods(nil, readerTypeName,
		map[string]lua.LGoFunc{"__tostring": readerToString}, readerMethods)
	walkerMetatable = value.RegisterTypeMethods(nil, walkerTypeName,
		map[string]lua.LGoFunc{"__tostring": walkerToString}, walkerMethods)
	writerMetatable = value.RegisterTypeMethods(nil, writerTypeName,
		map[string]lua.LGoFunc{"__tostring": writerToString}, writerMethods)
}

// Module is the archive module definition.
var Module = &luaapi.ModuleDef{
	Name:        "archive",
	Description: "Read and write zip/tar archives with bounded memory",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 4)
		mod.RawSetString("open", lua.LGoFunc(archiveOpen))
		mod.RawSetString("scan", lua.LGoFunc(archiveScan))
		mod.RawSetString("create", lua.LGoFunc(archiveCreate))
		mod.RawSetString("formats", lua.LGoFunc(archiveFormats))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func invalidError(l *lua.LState, msg string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, msg).WithKind(lua.Invalid).WithRetryable(false))
	return 2
}

func unavailableError(l *lua.LState, msg string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, msg).WithKind(lua.Unavailable).WithRetryable(false))
	return 2
}

func permissionError(l *lua.LState, msg string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, msg).WithKind(lua.PermissionDenied).WithRetryable(false))
	return 2
}

func notFoundError(l *lua.LState, msg string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, msg).WithKind(lua.NotFound).WithRetryable(false))
	return 2
}

func internalError(l *lua.LState, err error, ctx string) int {
	l.Push(lua.LNil)
	l.Push(lua.WrapErrorWithLua(l, err, ctx).WithKind(lua.Internal).WithRetryable(false))
	return 2
}

func archiveFormats(l *lua.LState) int {
	names := archiveapi.List()
	t := l.CreateTable(len(names), 0)
	for i, n := range names {
		t.RawSetInt(i+1, lua.LString(n))
	}
	l.Push(t)
	return 1
}

// parseOptions reads an options table from stack position idx (if present).
func parseOptions(l *lua.LState, idx int) archiveapi.Options {
	var o archiveapi.Options
	if idx > l.GetTop() || l.Get(idx).Type() != lua.LTTable {
		return o
	}
	t := l.ToTable(idx)
	if v := t.RawGetString("format"); v.Type() == lua.LTString {
		o.Format = v.String()
	}
	o.MaxEntries = optInt(t, "max_entries")
	o.MaxTotalBytes = optInt64(t, "max_total_bytes")
	o.MaxFileBytes = optInt64(t, "max_file_bytes")
	o.MaxInlineBytes = optInt64(t, "max_inline_bytes")
	o.BufferBytes = int(optInt64(t, "buffer_bytes"))
	return o
}

func optInt(t *lua.LTable, key string) int { return int(optInt64(t, key)) }

func optInt64(t *lua.LTable, key string) int64 {
	v := t.RawGetString(key)
	if v.Type() == lua.LTNumber || v.Type() == lua.LTInteger {
		return int64(lua.LVAsNumber(v))
	}
	return 0
}

func bufferSize(o archiveapi.Options) int {
	if o.BufferBytes > 0 {
		return o.BufferBytes
	}
	return sysarchive.DefaultBufferBytes
}

func maxTotalBytes(o archiveapi.Options) int64 {
	if o.MaxTotalBytes > 0 {
		return o.MaxTotalBytes
	}
	return sysarchive.DefaultMaxTotalBytes
}

type externalReaderAt interface {
	io.ReaderAt
	Size() int64
}

// seekableSource resolves arg1..argN into a random-access source for open().
// Returns the ReaderAt, total size, a name for sniffing, and a closer.
func seekableSource(l *lua.LState) (ra io.ReaderAt, size int64, name string, closer io.Closer, optsIdx int, errCode int) {
	switch v := l.Get(1).(type) {
	case lua.LString:
		data := []byte(string(v))
		return newBytesReaderAt(data), int64(len(data)), "", nil, 2, 0
	case *lua.LUserData:
		switch h := v.Value.(type) {
		case *fsmod.FS:
			path := l.CheckString(2)
			resolved, err := h.Resolve(path)
			if err != nil {
				return nil, 0, "", nil, 0, internalError(l, err, "resolve path")
			}
			f, err := h.Backend().OpenFile(resolved, os.O_RDONLY, 0)
			if err != nil {
				return nil, 0, "", nil, 0, internalError(l, err, "open source")
			}
			ra, ok := any(f).(io.ReaderAt)
			if !ok {
				_ = f.Close()
				return nil, 0, "", nil, 0, unavailableError(l, "source filesystem is not seekable")
			}
			info, err := f.Stat()
			if err != nil {
				_ = f.Close()
				return nil, 0, "", nil, 0, internalError(l, err, "stat source")
			}
			return ra, info.Size(), path, f, 3, 0
		case *fsmod.File:
			f := h.Backend()
			ra, ok := any(f).(io.ReaderAt)
			if !ok {
				return nil, 0, "", nil, 0, unavailableError(l, "source file is not seekable")
			}
			info, err := f.Stat()
			if err != nil {
				return nil, 0, "", nil, 0, internalError(l, err, "stat source")
			}
			return ra, info.Size(), "", nil, 2, 0
		default:
			if ext, ok := h.(externalReaderAt); ok {
				name := ""
				if named, nok := h.(interface{ Name() string }); nok {
					name = named.Name()
				}
				return ext, ext.Size(), name, nil, 2, 0
			}
		}
	}
	return nil, 0, "", nil, 0, invalidError(l, "source must be an fs handle, an fs file, bytes, or a random-access reader")
}

func resolveCodec(l *lua.LState, ra io.ReaderAt, name string, o archiveapi.Options) (archiveapi.Codec, int) {
	header := make([]byte, 512)
	n, _ := ra.ReadAt(header, 0)
	c, ok := archiveapi.Resolve(o.Format, name, header[:n])
	if !ok {
		return nil, invalidError(l, "unknown archive format (set opts.format)")
	}
	return c, 0
}

func archiveOpen(l *lua.LState) int {
	ctx := l.Context()
	if !security.IsAllowed(ctx, "archive.read", "", nil) {
		return permissionError(l, "not allowed to read archives")
	}

	ra, size, name, closer, optsIdx, code := seekableSource(l)
	if code != 0 {
		return code
	}
	o := parseOptions(l, optsIdx)

	c, code := resolveCodec(l, ra, name, o)
	if code != 0 {
		if closer != nil {
			_ = closer.Close()
		}
		return code
	}
	rc, ok := c.(archiveapi.RandomReadable)
	if !ok {
		if closer != nil {
			_ = closer.Close()
		}
		return unavailableError(l, "format "+c.Name()+" has no random access; use archive.scan")
	}

	reader, err := rc.OpenRandom(ra, size, o)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return internalError(l, err, "open archive")
	}

	pushReader(l, newLuaReader(ctx, reader, closer, o))
	l.Push(lua.LNil)
	return 2
}

func sysarchiveInline() int64 { return sysarchive.DefaultMaxInlineBytes }

func sysarchiveSanitize(name string) (string, bool) { return sysarchive.SanitizeEntryName(name) }
