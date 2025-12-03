package compress

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

const (
	defaultLevel     = 6
	minLevel         = 1
	maxLevel         = 9
	brotliMaxLevel   = 11
	zstdMaxLevel     = 22
	brotliMinLevel   = 0
	zstdDefaultLevel = 3
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton compress module instance.
var Module = &compressModule{}

type compressModule struct{}

func (m *compressModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "compress",
		Description: "Data compression (gzip, deflate, zlib, brotli, zstd)",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *compressModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}

		gzipTable := &lua.LTable{}
		gzipTable.RawSetString("encode", lua.LGoFunc(gzipEncode))
		gzipTable.RawSetString("decode", lua.LGoFunc(gzipDecode))
		gzipTable.Immutable = true
		mod.RawSetString("gzip", gzipTable)

		deflateTable := &lua.LTable{}
		deflateTable.RawSetString("encode", lua.LGoFunc(deflateEncode))
		deflateTable.RawSetString("decode", lua.LGoFunc(deflateDecode))
		deflateTable.Immutable = true
		mod.RawSetString("deflate", deflateTable)

		zlibTable := &lua.LTable{}
		zlibTable.RawSetString("encode", lua.LGoFunc(zlibEncode))
		zlibTable.RawSetString("decode", lua.LGoFunc(zlibDecode))
		zlibTable.Immutable = true
		mod.RawSetString("zlib", zlibTable)

		brotliTable := &lua.LTable{}
		brotliTable.RawSetString("encode", lua.LGoFunc(brotliEncode))
		brotliTable.RawSetString("decode", lua.LGoFunc(brotliDecode))
		brotliTable.Immutable = true
		mod.RawSetString("brotli", brotliTable)

		zstdTable := &lua.LTable{}
		zstdTable.RawSetString("encode", lua.LGoFunc(zstdEncode))
		zstdTable.RawSetString("decode", lua.LGoFunc(zstdDecode))
		zstdTable.Immutable = true
		mod.RawSetString("zstd", zstdTable)

		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *compressModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func getLevel(l *lua.LState, defaultVal, minVal, maxVal int) int {
	level := defaultVal
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.ToTable(2)
		lv := opts.RawGetString("level")
		// Check for both LNumber and LInteger (100 is parsed as integer)
		if lv.Type() == lua.LTNumber || lv.Type() == lua.LTInteger {
			level = int(lua.LVAsNumber(lv))
			if level < minVal || level > maxVal {
				return -1
			}
		}
	}
	return level
}

func gzipEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("compression level must be between 1 and 9"))
		return 2
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func gzipDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	reader, err := gzip.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func deflateEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("compression level must be between 1 and 9"))
		return 2
	}

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func deflateDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	reader := flate.NewReader(bytes.NewReader([]byte(data)))
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func zlibEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("compression level must be between 1 and 9"))
		return 2
	}

	var buf bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func zlibDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	reader, err := zlib.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func brotliEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	level := getLevel(l, defaultLevel, brotliMinLevel, brotliMaxLevel)
	if level < 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("brotli quality must be between 0 and 11"))
		return 2
	}

	var buf bytes.Buffer
	writer := brotli.NewWriterLevel(&buf, level)

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func brotliDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	reader := brotli.NewReader(bytes.NewReader([]byte(data)))
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func zstdEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	level := getLevel(l, zstdDefaultLevel, minLevel, zstdMaxLevel)
	if level < 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("zstd level must be between 1 and 22"))
		return 2
	}

	var encoderLevel zstd.EncoderLevel
	switch {
	case level <= 3:
		encoderLevel = zstd.SpeedFastest
	case level <= 6:
		encoderLevel = zstd.SpeedDefault
	case level <= 9:
		encoderLevel = zstd.SpeedBetterCompression
	default:
		encoderLevel = zstd.SpeedBestCompression
	}

	var buf bytes.Buffer
	writer, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(encoderLevel))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func zstdDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	data := l.ToString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input data cannot be empty"))
		return 2
	}

	reader, err := zstd.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}
