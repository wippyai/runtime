-- Test: hash error handling
local assert = require("assert2")
local hash = require("hash")

local function main()
    -- hash.md5 returns error on non-string
    local result, err = hash.md5(123)
    assert.is_nil(result, "md5 number nil result")
    assert.not_nil(err, "md5 number has error")
    assert.eq(err:kind(), errors.INVALID, "md5 number kind")
    assert.eq(err:retryable(), false, "md5 number not retryable")

    result, err = hash.md5(nil)
    assert.is_nil(result, "md5 nil nil result")
    assert.not_nil(err, "md5 nil has error")
    assert.eq(err:kind(), errors.INVALID, "md5 nil kind")

    -- hash.sha1 returns error on non-string
    result, err = hash.sha1(123)
    assert.is_nil(result, "sha1 number nil result")
    assert.not_nil(err, "sha1 number has error")
    assert.eq(err:kind(), errors.INVALID, "sha1 number kind")

    -- hash.sha256 returns error on non-string
    result, err = hash.sha256(123)
    assert.is_nil(result, "sha256 number nil result")
    assert.not_nil(err, "sha256 number has error")
    assert.eq(err:kind(), errors.INVALID, "sha256 number kind")

    -- hash.sha512 returns error on non-string
    result, err = hash.sha512(123)
    assert.is_nil(result, "sha512 number nil result")
    assert.not_nil(err, "sha512 number has error")
    assert.eq(err:kind(), errors.INVALID, "sha512 number kind")

    -- hash.fnv32 returns error on non-string
    result, err = hash.fnv32(123)
    assert.is_nil(result, "fnv32 number nil result")
    assert.not_nil(err, "fnv32 number has error")
    assert.eq(err:kind(), errors.INVALID, "fnv32 number kind")

    -- hash.fnv64 returns error on non-string
    result, err = hash.fnv64(123)
    assert.is_nil(result, "fnv64 number nil result")
    assert.not_nil(err, "fnv64 number has error")
    assert.eq(err:kind(), errors.INVALID, "fnv64 number kind")

    -- hash.hmac_md5 returns error on non-string data
    result, err = hash.hmac_md5(123, "secret")
    assert.is_nil(result, "hmac_md5 number data nil result")
    assert.not_nil(err, "hmac_md5 number data has error")
    assert.eq(err:kind(), errors.INVALID, "hmac_md5 number data kind")

    -- hash.hmac_md5 returns error on non-string secret
    result, err = hash.hmac_md5("hello", 123)
    assert.is_nil(result, "hmac_md5 number secret nil result")
    assert.not_nil(err, "hmac_md5 number secret has error")
    assert.eq(err:kind(), errors.INVALID, "hmac_md5 number secret kind")

    -- hash.hmac_sha1 returns error on non-string
    result, err = hash.hmac_sha1(123, "secret")
    assert.is_nil(result, "hmac_sha1 number data nil result")
    assert.not_nil(err, "hmac_sha1 number data has error")
    assert.eq(err:kind(), errors.INVALID, "hmac_sha1 number data kind")

    result, err = hash.hmac_sha1("hello", 123)
    assert.is_nil(result, "hmac_sha1 number secret nil result")
    assert.not_nil(err, "hmac_sha1 number secret has error")

    -- hash.hmac_sha256 returns error on non-string
    result, err = hash.hmac_sha256(123, "secret")
    assert.is_nil(result, "hmac_sha256 number data nil result")
    assert.not_nil(err, "hmac_sha256 number data has error")
    assert.eq(err:kind(), errors.INVALID, "hmac_sha256 number data kind")

    result, err = hash.hmac_sha256("hello", 123)
    assert.is_nil(result, "hmac_sha256 number secret nil result")
    assert.not_nil(err, "hmac_sha256 number secret has error")

    -- hash.hmac_sha512 returns error on non-string
    result, err = hash.hmac_sha512(123, "secret")
    assert.is_nil(result, "hmac_sha512 number data nil result")
    assert.not_nil(err, "hmac_sha512 number data has error")
    assert.eq(err:kind(), errors.INVALID, "hmac_sha512 number data kind")

    result, err = hash.hmac_sha512("hello", 123)
    assert.is_nil(result, "hmac_sha512 number secret nil result")
    assert.not_nil(err, "hmac_sha512 number secret has error")

    -- Verify errors have string representation
    local str = tostring(err)
    assert.not_nil(str, "error has tostring")
    assert.neq(str, "", "error string not empty")

    return true
end

return { main = main }
