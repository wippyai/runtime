package compress

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

const (
	DefaultCompressionLevel = 6
	MinCompressionLevel     = flate.BestSpeed
	MaxCompressionLevel     = flate.BestCompression
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewCompressModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "compress"
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.NewTable()

		gzipTable := l.NewTable()
		gzipTable.RawSetString("encode", l.NewFunction(m.gzipEncode))
		gzipTable.RawSetString("decode", l.NewFunction(m.gzipDecode))
		gzipTable.Immutable = true
		mod.RawSetString("gzip", gzipTable)

		deflateTable := l.NewTable()
		deflateTable.RawSetString("encode", l.NewFunction(m.deflateEncode))
		deflateTable.RawSetString("decode", l.NewFunction(m.deflateDecode))
		deflateTable.Immutable = true
		mod.RawSetString("deflate", deflateTable)

		zlibTable := l.NewTable()
		zlibTable.RawSetString("encode", l.NewFunction(m.zlibEncode))
		zlibTable.RawSetString("decode", l.NewFunction(m.zlibDecode))
		zlibTable.Immutable = true
		mod.RawSetString("zlib", zlibTable)

		brotliTable := l.NewTable()
		brotliTable.RawSetString("encode", l.NewFunction(m.brotliEncode))
		brotliTable.RawSetString("decode", l.NewFunction(m.brotliDecode))
		brotliTable.Immutable = true
		mod.RawSetString("brotli", brotliTable)

		zstdTable := l.NewTable()
		zstdTable.RawSetString("encode", l.NewFunction(m.zstdEncode))
		zstdTable.RawSetString("decode", l.NewFunction(m.zstdDecode))
		zstdTable.Immutable = true
		mod.RawSetString("zstd", zstdTable)

		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

func (m *Module) gzipEncode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "gzip"))
		return 2
	}

	level := DefaultCompressionLevel
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if lv := opts.RawGetString("level"); lv.Type() == lua.LTNumber {
			level = int(lua.LVAsNumber(lv))
			if level < MinCompressionLevel || level > MaxCompressionLevel {
				l.Push(lua.LNil)
				l.Push(newCompressInvalidError(l, fmt.Sprintf("compression level must be between %d and %d", MinCompressionLevel, MaxCompressionLevel), "gzip"))
				return 2
			}
		}
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create gzip writer: %w", err), "gzip", "encode"))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("compression failed: %w", err), "gzip", "encode"))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to finalize compression: %w", err), "gzip", "encode"))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) gzipDecode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "gzip"))
		return 2
	}

	reader, err := gzip.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create gzip reader: %w", err), "gzip", "decode"))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("decompression failed: %w", err), "gzip", "decode"))
		return 2
	}

	l.Push(lua.LString(string(decompressed)))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) deflateEncode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "deflate"))
		return 2
	}

	level := DefaultCompressionLevel
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if lv := opts.RawGetString("level"); lv.Type() == lua.LTNumber {
			level = int(lua.LVAsNumber(lv))
			if level < MinCompressionLevel || level > MaxCompressionLevel {
				l.Push(lua.LNil)
				l.Push(newCompressInvalidError(l, fmt.Sprintf("compression level must be between %d and %d", MinCompressionLevel, MaxCompressionLevel), "deflate"))
				return 2
			}
		}
	}

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create deflate writer: %w", err), "deflate", "encode"))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("compression failed: %w", err), "deflate", "encode"))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to finalize compression: %w", err), "deflate", "encode"))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) deflateDecode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "deflate"))
		return 2
	}

	reader := flate.NewReader(bytes.NewReader([]byte(data)))
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("decompression failed: %w", err), "deflate", "decode"))
		return 2
	}

	l.Push(lua.LString(string(decompressed)))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) zlibEncode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "zlib"))
		return 2
	}

	level := DefaultCompressionLevel
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if lv := opts.RawGetString("level"); lv.Type() == lua.LTNumber {
			level = int(lua.LVAsNumber(lv))
			if level < MinCompressionLevel || level > MaxCompressionLevel {
				l.Push(lua.LNil)
				l.Push(newCompressInvalidError(l, fmt.Sprintf("compression level must be between %d and %d", MinCompressionLevel, MaxCompressionLevel), "zlib"))
				return 2
			}
		}
	}

	var buf bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create zlib writer: %w", err), "zlib", "encode"))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("compression failed: %w", err), "zlib", "encode"))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to finalize compression: %w", err), "zlib", "encode"))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) zlibDecode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "gzip"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "zlib"))
		return 2
	}

	reader, err := zlib.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create zlib reader: %w", err), "zlib", "decode"))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("decompression failed: %w", err), "zlib", "decode"))
		return 2
	}

	l.Push(lua.LString(string(decompressed)))
	l.Push(lua.LNil)
	return 2
}
