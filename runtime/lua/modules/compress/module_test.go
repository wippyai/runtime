package compress

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("compress")
	if mod.Type() != lua.LTTable {
		t.Fatal("compress module not registered")
	}

	tbl := mod.(*lua.LTable)
	algos := []string{"gzip", "deflate", "zlib", "brotli", "zstd"}
	for _, algo := range algos {
		sub := tbl.RawGetString(algo)
		if sub.Type() != lua.LTTable {
			t.Errorf("%s submodule not registered", algo)
			continue
		}
		subTbl := sub.(*lua.LTable)
		if subTbl.RawGetString("encode").Type() != lua.LTFunction {
			t.Errorf("%s.encode not registered", algo)
		}
		if subTbl.RawGetString("decode").Type() != lua.LTFunction {
			t.Errorf("%s.decode not registered", algo)
		}
	}
}

func TestGzipRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! This is a test string for compression."
		local encoded, err = compress.gzip.encode(original)
		if not encoded then error(err) end
		local decoded, err = compress.gzip.decode(encoded)
		if not decoded then error(err) end
		if decoded ~= original then error("gzip round trip failed") end
	`)
	if err != nil {
		t.Errorf("gzip round trip failed: %v", err)
	}
}

func TestGzipWithLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.gzip.encode(data, {level = 9})
		if not encoded then error(err) end
		local decoded, err = compress.gzip.decode(encoded)
		if decoded ~= data then error("gzip with level failed") end
	`)
	if err != nil {
		t.Errorf("gzip with level failed: %v", err)
	}
}

func TestGzipInvalidLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.encode("test", {level = 100})
		if err == nil then error("expected error for invalid level") end
	`)
	if err != nil {
		t.Errorf("gzip invalid level test failed: %v", err)
	}
}

func TestGzipEmptyInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.encode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("gzip empty input test failed: %v", err)
	}
}

func TestGzipInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.encode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("gzip invalid input test failed: %v", err)
	}
}

func TestGzipDecodeInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.decode("not gzip data")
		if err == nil then error("expected error for invalid gzip") end
	`)
	if err != nil {
		t.Errorf("gzip decode invalid test failed: %v", err)
	}
}

func TestDeflateRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! This is a test string for compression."
		local encoded, err = compress.deflate.encode(original)
		if not encoded then error(err) end
		local decoded, err = compress.deflate.decode(encoded)
		if not decoded then error(err) end
		if decoded ~= original then error("deflate round trip failed") end
	`)
	if err != nil {
		t.Errorf("deflate round trip failed: %v", err)
	}
}

func TestDeflateEmptyInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.encode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("deflate empty input test failed: %v", err)
	}
}

func TestDeflateInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("deflate invalid input test failed: %v", err)
	}
}

func TestZlibRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! This is a test string for compression."
		local encoded, err = compress.zlib.encode(original)
		if not encoded then error(err) end
		local decoded, err = compress.zlib.decode(encoded)
		if not decoded then error(err) end
		if decoded ~= original then error("zlib round trip failed") end
	`)
	if err != nil {
		t.Errorf("zlib round trip failed: %v", err)
	}
}

func TestZlibEmptyInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.encode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("zlib empty input test failed: %v", err)
	}
}

func TestZlibDecodeInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.decode("not zlib data")
		if err == nil then error("expected error for invalid zlib") end
	`)
	if err != nil {
		t.Errorf("zlib decode invalid test failed: %v", err)
	}
}

func TestBrotliRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! This is a test string for compression."
		local encoded, err = compress.brotli.encode(original)
		if not encoded then error(err) end
		local decoded, err = compress.brotli.decode(encoded)
		if not decoded then error(err) end
		if decoded ~= original then error("brotli round trip failed") end
	`)
	if err != nil {
		t.Errorf("brotli round trip failed: %v", err)
	}
}

func TestBrotliWithLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.brotli.encode(data, {level = 11})
		if not encoded then error(err) end
		local decoded, err = compress.brotli.decode(encoded)
		if decoded ~= data then error("brotli with level failed") end
	`)
	if err != nil {
		t.Errorf("brotli with level failed: %v", err)
	}
}

func TestBrotliInvalidLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.encode("test", {level = 100})
		if err == nil then error("expected error for invalid level") end
	`)
	if err != nil {
		t.Errorf("brotli invalid level test failed: %v", err)
	}
}

func TestBrotliEmptyInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.encode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("brotli empty input test failed: %v", err)
	}
}

func TestZstdRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! This is a test string for compression."
		local encoded, err = compress.zstd.encode(original)
		if not encoded then error(err) end
		local decoded, err = compress.zstd.decode(encoded)
		if not decoded then error(err) end
		if decoded ~= original then error("zstd round trip failed") end
	`)
	if err != nil {
		t.Errorf("zstd round trip failed: %v", err)
	}
}

func TestZstdWithLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.zstd.encode(data, {level = 10})
		if not encoded then error(err) end
		local decoded, err = compress.zstd.decode(encoded)
		if decoded ~= data then error("zstd with level failed") end
	`)
	if err != nil {
		t.Errorf("zstd with level failed: %v", err)
	}
}

func TestZstdInvalidLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.encode("test", {level = 100})
		if err == nil then error("expected error for invalid level") end
	`)
	if err != nil {
		t.Errorf("zstd invalid level test failed: %v", err)
	}
}

func TestZstdEmptyInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.encode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("zstd empty input test failed: %v", err)
	}
}

func TestZstdDecodeInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.decode("not zstd data")
		if err == nil then error("expected error for invalid zstd") end
	`)
	if err != nil {
		t.Errorf("zstd decode invalid test failed: %v", err)
	}
}

func TestDeflateWithLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.deflate.encode(data, {level = 9})
		if not encoded then error(err) end
		local decoded, err = compress.deflate.decode(encoded)
		if decoded ~= data then error("deflate with level failed") end
	`)
	if err != nil {
		t.Errorf("deflate with level failed: %v", err)
	}
}

func TestDeflateInvalidLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.encode("test", {level = 100})
		if err == nil then error("expected error for invalid level") end
	`)
	if err != nil {
		t.Errorf("deflate invalid level test failed: %v", err)
	}
}

func TestZlibWithLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.zlib.encode(data, {level = 9})
		if not encoded then error(err) end
		local decoded, err = compress.zlib.decode(encoded)
		if decoded ~= data then error("zlib with level failed") end
	`)
	if err != nil {
		t.Errorf("zlib with level failed: %v", err)
	}
}

func TestZlibInvalidLevel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.encode("test", {level = 100})
		if err == nil then error("expected error for invalid level") end
	`)
	if err != nil {
		t.Errorf("zlib invalid level test failed: %v", err)
	}
}

func TestZlibInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.encode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("zlib invalid input test failed: %v", err)
	}
}

func TestBrotliInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.encode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("brotli invalid input test failed: %v", err)
	}
}

func TestBrotliDecodeInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.decode("not brotli data")
		if err == nil then error("expected error for invalid brotli") end
	`)
	if err != nil {
		t.Errorf("brotli decode invalid test failed: %v", err)
	}
}

func TestBrotliDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("brotli decode empty test failed: %v", err)
	}
}

func TestZstdInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.encode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("zstd invalid input test failed: %v", err)
	}
}

func TestDeflateDecodeInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.decode("not deflate data")
		if err == nil then error("expected error for invalid deflate") end
	`)
	if err != nil {
		t.Errorf("deflate decode invalid test failed: %v", err)
	}
}

func TestGzipDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("gzip decode empty test failed: %v", err)
	}
}

func TestDeflateDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("deflate decode empty test failed: %v", err)
	}
}

func TestZlibDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("zlib decode empty test failed: %v", err)
	}
}

func TestZstdDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("zstd decode empty test failed: %v", err)
	}
}

func TestZstdDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zstd.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("zstd decode invalid input test failed: %v", err)
	}
}

func TestBrotliDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.brotli.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("brotli decode invalid input test failed: %v", err)
	}
}

func TestZstdAllLevels(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		for _, level in ipairs({1, 4, 7, 15}) do
			local encoded, err = compress.zstd.encode(data, {level = level})
			if not encoded then error(err) end
			local decoded, err = compress.zstd.decode(encoded)
			if decoded ~= data then error("zstd level " .. level .. " failed") end
		end
	`)
	if err != nil {
		t.Errorf("zstd all levels test failed: %v", err)
	}
}

func TestGzipDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.gzip.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("gzip decode invalid input test failed: %v", err)
	}
}

func TestDeflateDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.deflate.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("deflate decode invalid input test failed: %v", err)
	}
}

func TestZlibDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = compress.zlib.decode(123)
		if err == nil then error("expected error for non-string") end
	`)
	if err != nil {
		t.Errorf("zlib decode invalid input test failed: %v", err)
	}
}

func TestBrotliLevelZero(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.brotli.encode(data, {level = 0})
		if not encoded then error(err) end
		local decoded, err = compress.brotli.decode(encoded)
		if decoded ~= data then error("brotli level 0 failed") end
	`)
	if err != nil {
		t.Errorf("brotli level zero test failed: %v", err)
	}
}

func TestGzipLevelOne(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local data = "Hello, World!"
		local encoded, err = compress.gzip.encode(data, {level = 1})
		if not encoded then error(err) end
		local decoded, err = compress.gzip.decode(encoded)
		if decoded ~= data then error("gzip level 1 failed") end
	`)
	if err != nil {
		t.Errorf("gzip level 1 test failed: %v", err)
	}
}

func TestBindReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Bind(l1)
	Bind(l2)

	mod1 := l1.GetGlobal("compress").(*lua.LTable)
	mod2 := l2.GetGlobal("compress").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}
