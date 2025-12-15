-- Test: crypto.hmac functions
local assert = require("assert2")

local function main()
    local crypto = require("crypto")

    -- hmac.sha256
    local digest, err = crypto.hmac.sha256("secret", "data")
    assert.is_nil(err, "hmac.sha256 should not error")
    assert.not_nil(digest, "hmac.sha256 returns digest")
    assert.eq(#digest, 64, "sha256 digest is 64 hex chars")

    -- Same key and data produce same result
    local digest2, _ = crypto.hmac.sha256("secret", "data")
    assert.eq(digest, digest2, "same inputs produce same digest")

    -- Different key produces different result
    local digest3, _ = crypto.hmac.sha256("other", "data")
    assert.neq(digest, digest3, "different key produces different digest")

    -- Different data produces different result
    local digest4, _ = crypto.hmac.sha256("secret", "other")
    assert.neq(digest, digest4, "different data produces different digest")

    -- hmac.sha512
    local digest5, err2 = crypto.hmac.sha512("secret", "data")
    assert.is_nil(err2, "hmac.sha512 should not error")
    assert.eq(#digest5, 128, "sha512 digest is 128 hex chars")

    -- Empty key should error
    local _, err3 = crypto.hmac.sha256("", "data")
    assert.not_nil(err3, "empty key should error")
    assert.eq(err3:kind(), errors.INVALID, "empty key error kind")
    assert.eq(err3:retryable(), false, "empty key not retryable")

    -- Empty data is allowed
    local digest6, err4 = crypto.hmac.sha256("secret", "")
    assert.is_nil(err4, "empty data should not error")
    assert.not_nil(digest6, "empty data produces digest")

    return true
end

return { main = main }
