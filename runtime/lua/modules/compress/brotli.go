package compress

import (
	"bytes"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
	lua "github.com/yuin/gopher-lua"
)

const (
	BrotliDefaultQuality = 6
	BrotliMinQuality     = 0
	BrotliMaxQuality     = 11
)

func (m *Module) brotliEncode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "brotli"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "brotli"))
		return 2
	}

	quality := BrotliDefaultQuality
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if lv := opts.RawGetString("level"); lv.Type() == lua.LTNumber {
			quality = int(lua.LVAsNumber(lv))
			if quality < BrotliMinQuality || quality > BrotliMaxQuality {
				l.Push(lua.LNil)
				l.Push(newCompressInvalidError(l, fmt.Sprintf("brotli quality must be between %d and %d", BrotliMinQuality, BrotliMaxQuality), "brotli"))
				return 2
			}
		}
	}

	var buf bytes.Buffer
	writer := brotli.NewWriterLevel(&buf, quality)

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("compression failed: %w", err), "brotli", "encode"))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to finalize compression: %w", err), "brotli", "encode"))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) brotliDecode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "brotli"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "brotli"))
		return 2
	}

	reader := brotli.NewReader(bytes.NewReader([]byte(data)))

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("decompression failed: %w", err), "brotli", "decode"))
		return 2
	}

	l.Push(lua.LString(decompressed))
	l.Push(lua.LNil)
	return 2
}
