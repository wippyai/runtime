// SPDX-License-Identifier: MPL-2.0

package compress

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zstd"
	lua "github.com/wippyai/go-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("compress")
	if mod.Type() != lua.LTTable {
		t.Fatal("compress module not registered")
	}

	modTbl := mod.(*lua.LTable)
	algos := []string{"gzip", "deflate", "zlib", "brotli", "zstd"}
	for _, algo := range algos {
		sub := modTbl.RawGetString(algo)
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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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

func TestZstdTrainDict(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local samples = {}
		for i = 1, 50 do
			table.insert(samples, string.format(
				'{"event":"click","user_id":%d,"session":"abc-%d","ts":17000000%02d}',
				i, i, i))
		end
		local dict, err = compress.zstd.train_dict(samples)
		if err then error(err) end
		if not dict then error("dict not returned") end
		if #dict < 64 then error("dict suspiciously small: " .. #dict) end
		local info, ierr = compress.zstd.inspect_dict(dict)
		if ierr then error(ierr) end
		if not info or info.content_size <= 0 then error("inspect_dict bad result") end
	`)
	if err != nil {
		t.Errorf("zstd train_dict test failed: %v", err)
	}
}

func TestZstdRoundTripWithDict(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local samples = {}
		for i = 1, 50 do
			table.insert(samples, string.format(
				'{"event":"click","user_id":%d,"session":"abc-%d","ts":17000000%02d}',
				i, i, i))
		end
		local dict, err = compress.zstd.train_dict(samples)
		if err then error(err) end

		local payload = '{"event":"click","user_id":4242,"session":"abc-4242","ts":1700000999}'
		local enc, eerr = compress.zstd.encode(payload, { dict = dict, level = 5 })
		if eerr then error(eerr) end
		local dec, derr = compress.zstd.decode(enc, { dict = dict })
		if derr then error(derr) end
		if dec ~= payload then error("round-trip with dict failed") end
	`)
	if err != nil {
		t.Errorf("zstd round-trip with dict test failed: %v", err)
	}
}

func TestZstdDictBetterThanNoDict(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local samples = {}
		for i = 1, 80 do
			table.insert(samples, string.format(
				'{"event":"click","user_id":%d,"session":"abc-%d","ts":17000000%02d}',
				i, i, i))
		end
		local dict, err = compress.zstd.train_dict(samples)
		if err then error(err) end

		local payload = '{"event":"click","user_id":9999,"session":"abc-9999","ts":1700000500}'
		local with_dict, e1 = compress.zstd.encode(payload, { dict = dict })
		if e1 then error(e1) end
		local without_dict, e2 = compress.zstd.encode(payload)
		if e2 then error(e2) end
		if #with_dict >= #without_dict then
			error(string.format("dict did not improve compression: %d >= %d",
				#with_dict, #without_dict))
		end
	`)
	if err != nil {
		t.Errorf("zstd dict-better-than-no-dict test failed: %v", err)
	}
}

func TestZstdDecodeWithWrongDict(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local function make_samples(prefix)
			local s = {}
			for i = 1, 40 do
				table.insert(s, prefix .. string.format('-event-%04d-payload-blob', i))
			end
			return s
		end
		local dict_a, e1 = compress.zstd.train_dict(make_samples("alpha"), { id = 1 })
		if e1 then error(e1) end
		local dict_b, e2 = compress.zstd.train_dict(make_samples("bravo"), { id = 2 })
		if e2 then error(e2) end

		local payload = "alpha-event-9999-payload-blob"
		local enc, eerr = compress.zstd.encode(payload, { dict = dict_a })
		if eerr then error(eerr) end

		local dec, derr = compress.zstd.decode(enc, { dict = dict_b })
		if derr == nil or dec ~= nil then
			error("expected decode failure with wrong dict")
		end
	`)
	if err != nil {
		t.Errorf("zstd decode-with-wrong-dict test failed: %v", err)
	}
}

func TestZstdDecodeWithoutDictFails(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local samples = {}
		for i = 1, 30 do
			table.insert(samples, string.format('event-%04d-large-payload-bytes', i))
		end
		local dict, terr = compress.zstd.train_dict(samples, { id = 42 })
		if terr then error(terr) end

		local payload = "event-1234-large-payload-bytes"
		local enc, eerr = compress.zstd.encode(payload, { dict = dict })
		if eerr then error(eerr) end

		local dec, derr = compress.zstd.decode(enc)
		if derr == nil or dec ~= nil then
			error("expected decode failure without dict")
		end
	`)
	if err != nil {
		t.Errorf("zstd decode-without-dict test failed: %v", err)
	}
}

func TestZstdTrainDictRejectsBadInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		-- samples not a table
		local _, err = compress.zstd.train_dict("not a table")
		if err == nil then error("expected error for non-table samples") end

		-- empty samples
		_, err = compress.zstd.train_dict({})
		if err == nil then error("expected error for empty samples") end

		-- non-string sample entry
		_, err = compress.zstd.train_dict({ 123 })
		if err == nil then error("expected error for non-string sample") end

		-- empty string sample
		_, err = compress.zstd.train_dict({ "" })
		if err == nil then error("expected error for empty sample") end

		-- samples too short overall (<8 bytes total)
		_, err = compress.zstd.train_dict({ "ab", "cd" })
		if err == nil then error("expected error for too-short total samples") end

		-- size out of range
		local big = string.rep("x", 32)
		_, err = compress.zstd.train_dict({ big, big, big }, { size = 100 })
		if err == nil then error("expected error for size below minimum") end
		_, err = compress.zstd.train_dict({ big, big, big }, { size = 2 * 1024 * 1024 })
		if err == nil then error("expected error for size above maximum") end

		-- id out of range
		_, err = compress.zstd.train_dict({ big, big, big }, { id = -1 })
		if err == nil then error("expected error for negative id") end

		-- level out of range
		_, err = compress.zstd.train_dict({ big, big, big }, { level = 99 })
		if err == nil then error("expected error for bad level") end
	`)
	if err != nil {
		t.Errorf("zstd train_dict bad-input test failed: %v", err)
	}
}

func TestZstdInspectDict(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local samples = {}
		for i = 1, 30 do
			table.insert(samples, string.format('record-%04d-with-shared-tail', i))
		end
		local dict, terr = compress.zstd.train_dict(samples, { id = 0xCAFE })
		if terr then error(terr) end

		local info, ierr = compress.zstd.inspect_dict(dict)
		if ierr then error(ierr) end
		if info.id ~= 0xCAFE then
			error("inspect_dict id mismatch: " .. tostring(info.id))
		end
		if info.content_size <= 0 then
			error("inspect_dict content_size <= 0")
		end

		-- inspect_dict rejects bogus bytes
		local _, e = compress.zstd.inspect_dict("not a dictionary at all")
		if e == nil then error("expected error for invalid dict bytes") end

		-- inspect_dict rejects empty / non-string
		local _, e2 = compress.zstd.inspect_dict("")
		if e2 == nil then error("expected error for empty dict") end
		local _, e3 = compress.zstd.inspect_dict(123)
		if e3 == nil then error("expected error for non-string dict") end
	`)
	if err != nil {
		t.Errorf("zstd inspect_dict test failed: %v", err)
	}
}

func TestZstdEncodeInvalidDictOption(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local _, err = compress.zstd.encode("hello world", { dict = 123 })
		if err == nil then error("expected error for non-string dict") end

		local _, derr = compress.zstd.decode("\x28\xb5\x2f\xfd", { dict = "" })
		if derr == nil then error("expected error for empty dict") end
	`)
	if err != nil {
		t.Errorf("zstd invalid dict option test failed: %v", err)
	}
}

// TestZstdDictProofOfWork is the end-to-end demonstration:
//  1. Train a dict in Lua state #1 from 200 structurally-similar JSON-ish events.
//  2. Encode the same payload with the dict (Lua state #1) and without it (Lua
//     state #1) and report concrete byte counts to t.Log.
//  3. Move the trained dict bytes verbatim into a fresh Lua state #2 and decode
//     there — proves the dict bytes are portable across process/state boundaries.
//  4. Decode the dict-encoded frame directly with the underlying klauspost
//     decoder using the same dict bytes — proves the output is a real, portable
//     zstd dictionary, not something only our wrapper can read.
func TestZstdDictProofOfWork(t *testing.T) {
	const sampleCount = 200

	// --- Lua state #1: train, encode-with-dict, encode-without-dict ---
	l1 := lua.NewState()
	defer l1.Close()
	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)

	if err := l1.DoString(`
		samples = {}
		for i = 1, ` + itoa(sampleCount) + ` do
			table.insert(samples, string.format(
				'{"event":"click","user_id":%d,"session":"sess-%04d","page":"/dashboard","ts":17000000%02d}',
				i, i, i % 100))
		end
		dict, terr = compress.zstd.train_dict(samples)
		if terr then error(terr) end
		payload = '{"event":"click","user_id":424242,"session":"sess-4242","page":"/dashboard","ts":1700000099}'
		enc_with,    e1 = compress.zstd.encode(payload, { dict = dict, level = 5 })
		enc_without, e2 = compress.zstd.encode(payload, { level = 5 })
		if e1 then error(e1) end
		if e2 then error(e2) end
	`); err != nil {
		t.Fatalf("training/encoding in state #1 failed: %v", err)
	}

	dictVal := l1.GetGlobal("dict")
	encWithVal := l1.GetGlobal("enc_with")
	encWithoutVal := l1.GetGlobal("enc_without")
	payloadVal := l1.GetGlobal("payload")
	if dictVal.Type() != lua.LTString || encWithVal.Type() != lua.LTString ||
		encWithoutVal.Type() != lua.LTString || payloadVal.Type() != lua.LTString {
		t.Fatal("expected string globals from state #1")
	}
	dictBytes := lua.LVAsString(dictVal)
	encWith := lua.LVAsString(encWithVal)
	encWithout := lua.LVAsString(encWithoutVal)
	payload := lua.LVAsString(payloadVal)

	t.Logf("sample_count=%d", sampleCount)
	t.Logf("dict_bytes=%d", len(dictBytes))
	t.Logf("payload_bytes=%d", len(payload))
	t.Logf("frame_with_dict_bytes=%d", len(encWith))
	t.Logf("frame_without_dict_bytes=%d", len(encWithout))
	t.Logf("ratio_with_dict=%.3f", float64(len(encWith))/float64(len(payload)))
	t.Logf("ratio_without_dict=%.3f", float64(len(encWithout))/float64(len(payload)))

	if len(encWith) >= len(encWithout) {
		t.Fatalf("dict did not improve compression: with=%d >= without=%d",
			len(encWith), len(encWithout))
	}
	if len(encWith) == 0 || len(encWithout) == 0 || len(dictBytes) == 0 {
		t.Fatal("unexpected empty output")
	}

	// --- Lua state #2: load dict bytes from outside, decode ---
	l2 := lua.NewState()
	defer l2.Close()
	l2.SetGlobal(Module.Name, tbl)
	l2.SetGlobal("dict", lua.LString(dictBytes))
	l2.SetGlobal("enc_with", lua.LString(encWith))
	l2.SetGlobal("payload", lua.LString(payload))

	if err := l2.DoString(`
		local dec, derr = compress.zstd.decode(enc_with, { dict = dict })
		if derr then error(derr) end
		if dec ~= payload then error("cross-state decode mismatch") end

		-- Decoding the dict-compressed frame without the dict must fail.
		local bad, berr = compress.zstd.decode(enc_with)
		if berr == nil or bad ~= nil then
			error("expected failure decoding without dict")
		end
	`); err != nil {
		t.Fatalf("cross-state decode in state #2 failed: %v", err)
	}
	t.Log("cross_state_decode=ok")

	// --- External proof: decode the same frame using klauspost directly ---
	dec, err := zstd.NewReader(bytes.NewReader([]byte(encWith)),
		zstd.WithDecoderDicts([]byte(dictBytes)))
	if err != nil {
		t.Fatalf("klauspost decoder rejected our dict: %v", err)
	}
	defer dec.Close()
	got, err := decodeAll(dec)
	if err != nil {
		t.Fatalf("klauspost decode failed: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("klauspost decode mismatch:\n got: %q\nwant: %q", got, payload)
	}
	t.Log("external_klauspost_decode=ok")

	// --- External proof in reverse: encode with klauspost using our dict,
	//     decode in Lua state #2 with the same dict. ---
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderDict([]byte(dictBytes)))
	if err != nil {
		t.Fatalf("klauspost encoder rejected our dict: %v", err)
	}
	externalFrame := enc.EncodeAll([]byte(payload), nil)
	_ = enc.Close()

	l2.SetGlobal("ext_frame", lua.LString(externalFrame))
	if err := l2.DoString(`
		local out, e = compress.zstd.decode(ext_frame, { dict = dict })
		if e then error(e) end
		if out ~= payload then error("external-encoded decode mismatch") end
	`); err != nil {
		t.Fatalf("decoding klauspost-produced frame in Lua failed: %v", err)
	}
	t.Logf("external_klauspost_encode_bytes=%d", len(externalFrame))
	t.Log("external_klauspost_encode_then_lua_decode=ok")
}

func decodeAll(r *zstd.Decoder) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("compress").(*lua.LTable)
	mod2 := l2.GetGlobal("compress").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}
