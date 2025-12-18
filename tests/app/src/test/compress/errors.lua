-- Test: Compression error handling
local assert = require("assert2")

local function main()
    local compress = require("compress")

    -- Empty input errors
    local result, err = compress.gzip.encode("")
    assert.is_nil(result, "empty encode returns nil")
    assert.not_nil(err, "empty encode returns error")
    assert.eq(err:kind(), errors.INVALID, "empty encode error kind")
    assert.eq(err:retryable(), false, "empty encode not retryable")
    assert.contains(tostring(err), "empty", "error mentions empty")

    result, err = compress.gzip.decode("")
    assert.is_nil(result, "empty decode returns nil")
    assert.not_nil(err, "empty decode returns error")
    assert.eq(err:kind(), errors.INVALID, "empty decode error kind")

    -- Invalid compressed data
    result, err = compress.gzip.decode("not valid gzip data")
    assert.is_nil(result, "invalid gzip returns nil")
    assert.not_nil(err, "invalid gzip returns error")
    assert.eq(err:kind(), errors.INVALID, "invalid gzip error kind")

    result, err = compress.deflate.decode("not valid deflate data")
    assert.is_nil(result, "invalid deflate returns nil")
    assert.not_nil(err, "invalid deflate returns error")
    assert.eq(err:kind(), errors.INVALID, "invalid deflate error kind")

    result, err = compress.zlib.decode("not valid zlib data")
    assert.is_nil(result, "invalid zlib returns nil")
    assert.not_nil(err, "invalid zlib returns error")
    assert.eq(err:kind(), errors.INVALID, "invalid zlib error kind")

    result, err = compress.brotli.decode("not valid brotli data")
    assert.is_nil(result, "invalid brotli returns nil")
    assert.not_nil(err, "invalid brotli returns error")
    assert.eq(err:kind(), errors.INVALID, "invalid brotli error kind")

    result, err = compress.zstd.decode("not valid zstd data")
    assert.is_nil(result, "invalid zstd returns nil")
    assert.not_nil(err, "invalid zstd returns error")
    assert.eq(err:kind(), errors.INTERNAL, "invalid zstd error kind")

    return true
end

return { main = main }
