// SPDX-License-Identifier: MPL-2.0

package compress

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

const (
	defaultLevel     = 6
	minLevel         = 1
	maxLevel         = 9
	brotliMaxLevel   = 11
	zstdMaxLevel     = 22
	brotliMinLevel   = 0
	zstdDefaultLevel = 3
	defaultMaxSize   = 128 * 1024 * 1024  // 128MB default decompression limit
	absoluteMaxSize  = 1024 * 1024 * 1024 // 1GB absolute maximum
)

// Module is the compress module definition.
var Module = &luaapi.ModuleDef{
	Name:        "compress",
	Description: "Data compression (gzip, deflate, zlib, brotli, zstd)",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 5)

		gzipTable := lua.CreateTable(0, 2)
		gzipTable.RawSetString("encode", lua.LGoFunc(gzipEncode))
		gzipTable.RawSetString("decode", lua.LGoFunc(gzipDecode))
		gzipTable.Immutable = true
		mod.RawSetString("gzip", gzipTable)

		deflateTable := lua.CreateTable(0, 2)
		deflateTable.RawSetString("encode", lua.LGoFunc(deflateEncode))
		deflateTable.RawSetString("decode", lua.LGoFunc(deflateDecode))
		deflateTable.Immutable = true
		mod.RawSetString("deflate", deflateTable)

		zlibTable := lua.CreateTable(0, 2)
		zlibTable.RawSetString("encode", lua.LGoFunc(zlibEncode))
		zlibTable.RawSetString("decode", lua.LGoFunc(zlibDecode))
		zlibTable.Immutable = true
		mod.RawSetString("zlib", zlibTable)

		brotliTable := lua.CreateTable(0, 2)
		brotliTable.RawSetString("encode", lua.LGoFunc(brotliEncode))
		brotliTable.RawSetString("decode", lua.LGoFunc(brotliDecode))
		brotliTable.Immutable = true
		mod.RawSetString("brotli", brotliTable)

		zstdTable := lua.CreateTable(0, 2)
		zstdTable.RawSetString("encode", lua.LGoFunc(zstdEncode))
		zstdTable.RawSetString("decode", lua.LGoFunc(zstdDecode))
		zstdTable.Immutable = true
		mod.RawSetString("zstd", zstdTable)

		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func invalidInputError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func getLevel(l *lua.LState, defaultVal, minVal, maxVal int) int {
	level := defaultVal
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.ToTable(2)
		lv := opts.RawGetString("level")
		if lv.Type() == lua.LTNumber || lv.Type() == lua.LTInteger {
			level = int(lua.LVAsNumber(lv))
			if level < minVal || level > maxVal {
				return -1
			}
		}
	}
	return level
}

func getMaxSize(l *lua.LState) int64 {
	maxSize := int64(defaultMaxSize)
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.ToTable(2)
		lv := opts.RawGetString("max_size")
		if lv.Type() == lua.LTNumber || lv.Type() == lua.LTInteger {
			v := int64(lua.LVAsNumber(lv))
			if v > 0 && v <= absoluteMaxSize {
				maxSize = v
			}
		}
	}
	return maxSize
}

func limitedReadAll(r io.Reader, maxSize int64) ([]byte, error) {
	limited := io.LimitReader(r, maxSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("decompressed size exceeds limit")
	}
	return data, nil
}

func gzipEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		return invalidInputError(l, "compression level must be between 1 and 9")
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return internalError(l, err, "gzip encode failed")
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		_ = writer.Close()
		return internalError(l, err, "gzip encode failed")
	}

	if err := writer.Close(); err != nil {
		return internalError(l, err, "gzip encode failed")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func gzipDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	maxSize := getMaxSize(l)

	reader, err := gzip.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		return invalidInputError(l, "invalid gzip data")
	}
	defer func() { _ = reader.Close() }()

	decompressed, err := limitedReadAll(reader, maxSize)
	if err != nil {
		return internalError(l, err, "gzip decode failed")
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func deflateEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		return invalidInputError(l, "compression level must be between 1 and 9")
	}

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, level)
	if err != nil {
		return internalError(l, err, "deflate encode failed")
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		_ = writer.Close()
		return internalError(l, err, "deflate encode failed")
	}

	if err := writer.Close(); err != nil {
		return internalError(l, err, "deflate encode failed")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func deflateDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	maxSize := getMaxSize(l)

	reader := flate.NewReader(bytes.NewReader([]byte(data)))
	defer func() { _ = reader.Close() }()

	decompressed, err := limitedReadAll(reader, maxSize)
	if err != nil {
		return invalidInputError(l, "invalid deflate data")
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func zlibEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	level := getLevel(l, defaultLevel, minLevel, maxLevel)
	if level < 0 {
		return invalidInputError(l, "compression level must be between 1 and 9")
	}

	var buf bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		return internalError(l, err, "zlib encode failed")
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		_ = writer.Close()
		return internalError(l, err, "zlib encode failed")
	}

	if err := writer.Close(); err != nil {
		return internalError(l, err, "zlib encode failed")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func zlibDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	maxSize := getMaxSize(l)

	reader, err := zlib.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		return invalidInputError(l, "invalid zlib data")
	}
	defer func() { _ = reader.Close() }()

	decompressed, err := limitedReadAll(reader, maxSize)
	if err != nil {
		return internalError(l, err, "zlib decode failed")
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func brotliEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	level := getLevel(l, defaultLevel, brotliMinLevel, brotliMaxLevel)
	if level < 0 {
		return invalidInputError(l, "brotli quality must be between 0 and 11")
	}

	var buf bytes.Buffer
	writer := brotli.NewWriterLevel(&buf, level)

	if _, err := writer.Write([]byte(data)); err != nil {
		_ = writer.Close()
		return internalError(l, err, "brotli encode failed")
	}

	if err := writer.Close(); err != nil {
		return internalError(l, err, "brotli encode failed")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func brotliDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	maxSize := getMaxSize(l)

	reader := brotli.NewReader(bytes.NewReader([]byte(data)))
	decompressed, err := limitedReadAll(reader, maxSize)
	if err != nil {
		return invalidInputError(l, "invalid brotli data")
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}

func zstdEncode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	level := getLevel(l, zstdDefaultLevel, minLevel, zstdMaxLevel)
	if level < 0 {
		return invalidInputError(l, "zstd level must be between 1 and 22")
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
		return internalError(l, err, "zstd encode failed")
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		_ = writer.Close()
		return internalError(l, err, "zstd encode failed")
	}

	if err := writer.Close(); err != nil {
		return internalError(l, err, "zstd encode failed")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func zstdDecode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidInputError(l, "string expected")
	}

	data := l.ToString(1)
	if data == "" {
		return invalidInputError(l, "input data cannot be empty")
	}

	maxSize := getMaxSize(l)

	reader, err := zstd.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		return invalidInputError(l, "invalid zstd data")
	}
	defer reader.Close()

	decompressed, err := limitedReadAll(reader, maxSize)
	if err != nil {
		return internalError(l, err, "zstd decode failed")
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}
