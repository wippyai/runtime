package compress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestCompressModule(t *testing.T) {
	t.Run("module loading", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")
			assert(type(compress) == "table")
			assert(type(compress.gzip) == "table")
			assert(type(compress.gzip.encode) == "function")
			assert(type(compress.gzip.decode) == "function")
			assert(type(compress.deflate) == "table")
			assert(type(compress.deflate.encode) == "function")
			assert(type(compress.deflate.decode) == "function")
			assert(type(compress.zlib) == "table")
			assert(type(compress.zlib.encode) == "function")
			assert(type(compress.zlib.decode) == "function")
			assert(type(compress.brotli) == "table")
			assert(type(compress.brotli.encode) == "function")
			assert(type(compress.brotli.decode) == "function")
			assert(type(compress.zstd) == "table")
			assert(type(compress.zstd.encode) == "function")
			assert(type(compress.zstd.decode) == "function")
		`)
		assert.NoError(t, err)
	})

	t.Run("gzip encode/decode", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = string.rep("Hello, World! This is a test string for gzip compression. ", 10)

			local compressed, err = compress.gzip.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))
			assert(#compressed < #data, "compressed data should be smaller")

			local decompressed, err = compress.gzip.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")

			return "ok"
		`)
		require.NoError(t, err)
	})

	t.Run("gzip with compression level", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = string.rep("Hello, World! ", 100)

			local compressed_fast, err = compress.gzip.encode(data, {level = 1})
			assert(err == nil, "fast encoding error: " .. tostring(err))

			local compressed_best, err = compress.gzip.encode(data, {level = 9})
			assert(err == nil, "best encoding error: " .. tostring(err))

			assert(#compressed_best <= #compressed_fast, "best compression should be smaller or equal")

			local decompressed, err = compress.gzip.decode(compressed_best)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")
		`)
		assert.NoError(t, err)
	})

	t.Run("deflate encode/decode", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = "Deflate compression test data with some repeating patterns."

			local compressed, err = compress.deflate.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))

			local decompressed, err = compress.deflate.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")
		`)
		assert.NoError(t, err)
	})

	t.Run("zlib encode/decode", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = "Zlib compression test with checksum validation."

			local compressed, err = compress.zlib.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))

			local decompressed, err = compress.zlib.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")
		`)
		assert.NoError(t, err)
	})

	t.Run("brotli encode/decode", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = string.rep("Brotli is a modern compression algorithm. ", 10)

			local compressed, err = compress.brotli.encode(data, {level = 6})
			assert(err == nil, "encoding error: " .. tostring(err))
			assert(#compressed < #data, "compressed data should be smaller")

			local decompressed, err = compress.brotli.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")
		`)
		assert.NoError(t, err)
	})

	t.Run("zstd encode/decode", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = string.rep("Zstandard offers excellent compression ratios. ", 20)

			local compressed, err = compress.zstd.encode(data, {level = 3})
			assert(err == nil, "encoding error: " .. tostring(err))
			assert(#compressed < #data, "compressed data should be smaller")

			local decompressed, err = compress.zstd.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "decompressed data should match original")
		`)
		assert.NoError(t, err)
	})

	t.Run("error on empty input", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local compressed, err = compress.gzip.encode("")
			assert(compressed == nil, "should return nil for empty input")
			assert(err ~= nil, "should return error for empty input")
		`)
		assert.NoError(t, err)
	})

	t.Run("error on invalid compression level", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = "test data"
			local compressed, err = compress.gzip.encode(data, {level = 100})
			assert(compressed == nil, "should return nil for invalid level")
			assert(err ~= nil, "should return error for invalid level")
		`)
		assert.NoError(t, err)
	})

	t.Run("error on invalid compressed data", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local decompressed, err = compress.gzip.decode("not valid gzip data")
			assert(decompressed == nil, "should return nil for invalid data")
			assert(err ~= nil, "should return error for invalid data")
		`)
		assert.NoError(t, err)
	})

	t.Run("large data compression", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = string.rep("Large data block for testing compression efficiency. ", 1000)

			local compressed_gzip, err = compress.gzip.encode(data)
			assert(err == nil, "gzip encoding error: " .. tostring(err))

			local compressed_zstd, err = compress.zstd.encode(data)
			assert(err == nil, "zstd encoding error: " .. tostring(err))

			assert(#compressed_gzip < #data * 0.5, "gzip should compress well")
			assert(#compressed_zstd < #data * 0.5, "zstd should compress well")

			local decompressed, err = compress.gzip.decode(compressed_gzip)
			assert(err == nil, "gzip decoding error: " .. tostring(err))
			assert(decompressed == data, "gzip decompressed data should match")

			decompressed, err = compress.zstd.decode(compressed_zstd)
			assert(err == nil, "zstd decoding error: " .. tostring(err))
			assert(decompressed == data, "zstd decompressed data should match")
		`)
		assert.NoError(t, err)
	})

	t.Run("binary data handling", func(t *testing.T) {
		mod := NewCompressModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Info().Name, mod.Loader)

		err := L.DoString(`
			local compress = require("compress")

			local data = ""
			for i = 0, 255 do
				data = data .. string.char(i)
			end

			local compressed, err = compress.gzip.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))

			local decompressed, err = compress.gzip.decode(compressed)
			assert(err == nil, "decoding error: " .. tostring(err))
			assert(decompressed == data, "binary data should be preserved")
		`)
		assert.NoError(t, err)
	})
}
