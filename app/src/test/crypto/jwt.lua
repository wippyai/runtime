-- Test: crypto.jwt functions
local assert = require("assert2")

local function main()
    local crypto = require("crypto")

    -- Basic JWT encode/verify
    local exp = os.time() + 3600 -- 1 hour from now
    local payload = { sub = "user123", name = "Test User", exp = exp }
    local token, err = crypto.jwt.encode(payload, "secret")
    assert.is_nil(err, "jwt.encode should not error")
    assert.not_nil(token, "jwt.encode returns token")
    assert.contains(token, ".", "jwt contains dots")

    local decoded, err2 = crypto.jwt.verify(token, "secret")
    assert.is_nil(err2, "jwt.verify should not error")
    assert.not_nil(decoded, "jwt.verify returns payload")
    assert.eq(decoded.sub, "user123", "sub claim preserved")
    assert.eq(decoded.name, "Test User", "name claim preserved")

    -- Wrong key should fail verification
    local _, err3 = crypto.jwt.verify(token, "wrong_secret")
    assert.not_nil(err3, "wrong key should fail verification")

    -- Invalid token should fail
    local _, err4 = crypto.jwt.verify("invalid.token.here", "secret")
    assert.not_nil(err4, "invalid token should fail")

    -- HS384 algorithm
    local t384, err5 = crypto.jwt.encode(payload, "secret", "HS384")
    assert.is_nil(err5, "HS384 encode should not error")
    local d384, err6 = crypto.jwt.verify(t384, "secret", "HS384")
    assert.is_nil(err6, "HS384 verify should not error")
    assert.eq(d384.sub, "user123", "HS384 preserves claims")

    -- HS512 algorithm
    local t512, err7 = crypto.jwt.encode(payload, "secret", "HS512")
    assert.is_nil(err7, "HS512 encode should not error")
    local d512, err8 = crypto.jwt.verify(t512, "secret", "HS512")
    assert.is_nil(err8, "HS512 verify should not error")
    assert.eq(d512.sub, "user123", "HS512 preserves claims")

    -- Algorithm mismatch should fail
    local _, err9 = crypto.jwt.verify(t384, "secret", "HS256")
    assert.not_nil(err9, "algorithm mismatch should fail")

    -- Custom header via _header field
    local payload_header = {
        sub = "user123",
        exp = exp,
        _header = { kid = "key-123" }
    }
    local t_header, err10 = crypto.jwt.encode(payload_header, "secret")
    assert.is_nil(err10, "jwt with custom header should not error")
    local d_header, err11 = crypto.jwt.verify(t_header, "secret")
    assert.is_nil(err11, "jwt with custom header verifies")
    assert.eq(d_header.sub, "user123", "claims preserved with custom header")

    return true
end

return { main = main }
