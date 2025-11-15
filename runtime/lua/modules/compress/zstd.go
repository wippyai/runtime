package compress

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	lua "github.com/yuin/gopher-lua"
)

const (
	ZstdDefaultLevel = 3
	ZstdMinLevel     = 1
	ZstdMaxLevel     = 22
)

func (m *Module) zstdEncode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "zstd"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "zstd"))
		return 2
	}

	level := ZstdDefaultLevel
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if lv := opts.RawGetString("level"); lv.Type() == lua.LTNumber {
			level = int(lua.LVAsNumber(lv))
			if level < ZstdMinLevel || level > ZstdMaxLevel {
				l.Push(lua.LNil)
				l.Push(newCompressInvalidError(l, fmt.Sprintf("zstd level must be between %d and %d", ZstdMinLevel, ZstdMaxLevel), "zstd"))
				return 2
			}
		}
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
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create zstd writer: %w", err), "zstd", "encode"))
		return 2
	}

	if _, err := writer.Write([]byte(data)); err != nil {
		writer.Close()
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("compression failed: %w", err), "zstd", "encode"))
		return 2
	}

	if err := writer.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to finalize compression: %w", err), "zstd", "encode"))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) zstdDecode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "missing input data", "zstd"))
		return 2
	}

	data := l.CheckString(1)
	if data == "" {
		l.Push(lua.LNil)
		l.Push(newCompressInvalidError(l, "input data cannot be empty", "zstd"))
		return 2
	}

	reader, err := zstd.NewReader(bytes.NewReader([]byte(data)))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("failed to create zstd reader: %w", err), "zstd", "decode"))
		return 2
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newCompressOperationError(l, fmt.Errorf("decompression failed: %w", err), "zstd", "decode"))
		return 2
	}

	l.Push(lua.LString(string(decompressed)))
	l.Push(lua.LNil)
	return 2
}
