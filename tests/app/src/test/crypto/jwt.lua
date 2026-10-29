-- SPDX-License-Identifier: MPL-2.0

-- Test: crypto.jwt functions
local assert = require("assert2")

local RSA_PRIVATE_KEY = [[
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC9f06DQlzCZrC8
If7AKRpmyf4bzJTxvpc61b02BpWwOe+JhubCoFmDPtGfyhBWGbTOhdY4VnZzgC8o
4PPRS3JfUqvU8380oX8LwGjHfjFH8VsomYf9gBxQQmS6ht1RypYkqAHkZZeXafXn
2oMpeW8ag8CZLcMFXnfCOFeZTbT1pvMR8DlxdAj9kHPQaq1hRaQkNaLGgVjl7Pxg
d5Q2wwbbVpZU1qeyTzzLHM14CPKyM9Wo60oO1L7lPWZBW1iN4jHfoarEc2l/NCXe
4DbQ8grXoLhDG1XjcCjcoeaF5m/dhBwHO4H07jLgE0/6YPOvjKHaQb+btgEOZgpq
f3QneQHJAgMBAAECggEAA5wQEm1CT9Qtr+m8XOvzhCt7Fwu4txmkrxLY97VcptaV
ruKWVPUBu3+n9dGfKcqz0F7Jh7ly6GyTyWUuk8kqgLEWE5h/TvD4FvDXdhxb4zVQ
YZe6GMy2zVsUaN6uitMvdLuMDK4YOwLCU8QsbS7yXDiQA8L0xO6JnLYeyg1qQecf
rdvuuj8GWDUQljK7IvzGfTvvbZInXXED1A93bXWrZ0ZZVIMWwWP9/LkCr9Vpjz6i
gh8W3MsS34fYygEgNGQzxmPKDB4rb8dwRvpAvOaxX+0jfRa/lRCE4uA7JMXH5v2G
B5LB3Ni/fKdNCFpNUcEUGTD+GG3ipm+1cfRSW6jhsQKBgQDuCYqnfMrMh4Tn6xUF
krXOjiqoXKLGcKFZopmxQBw9LROdxvlQng/lUI1wgCcnzYGDe1RQ2WMeF0+pHS+Y
lQDfX7vTgavN4xBRnfDxN2sHrGNWmdYdylnOlk8fC8Gq+ucdKfNYtA/130M71dPp
OzLC6ut2NQ21BH0aMYv9XvSzGQKBgQDLzA8Dp3P6JsESmKbEfz6oi+HHHrisbkvp
4mKRLsTBcC1du1Q/4pifO89jW3kGgohIIOgoUj5AHqd2rh8LThQrmFAuR4dVfNd0
tbbB6lZ7sq767W6bhEpsjBK+P5ZDYZ2u/ylc7zqaiKLSxZ0GwHOjDSfU41DysR6P
2+YLgvnKMQKBgGdZETAiyFIAzL18nYXr6q+hnfe3MJ0DraojJKOGeDw/Y/LuvrQ3
zCL91Erx+Q4Y9ZBGbZckqxoXeOit2Upf9x8Ei6IK/LLJUP5+mmvAaeHNANxKsmIA
xvkg3X1XN2Cqyskou5TZqMxRgPlrnzP2FdX29QA/jJH5Pu+ub180aP2RAoGANmwG
smYcPXft5SZoml6Uf/Pi1gapMDrAAPQYeEax+ioXo0ksJWdAdH1HU3QQ7KEc/Y30
ERhtUTgeZj33QlCWaFHjT4iAZl7RNnR0GnKWp6++tHmFKPh522TIeI+Ic/P0ZM/0
3sFvt+a2/GwTE8N+3dLciSgu7LBJxgbl9rsCM3ECgYEAtBdhAIBPvnr3FerfuBOk
vtKgWQRgg68UzQEX0KU/49QjvkQVbMssOu8i0Zzz0xSrsy94u3kqlKqNBLYjmHuY
1fMKlmeSX9pQFaj93a5HTq7KWWVmmugM5llDN3eyRTMXaLhdgXSivJVmo35RjWA2
IJmP/7TZCCqUDF//zsaFtGM=
-----END PRIVATE KEY-----
]]

local RSA_PUBLIC_KEY = [[
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvX9Og0JcwmawvCH+wCka
Zsn+G8yU8b6XOtW9NgaVsDnviYbmwqBZgz7Rn8oQVhm0zoXWOFZ2c4AvKODz0Uty
X1Kr1PN/NKF/C8Box34xR/FbKJmH/YAcUEJkuobdUcqWJKgB5GWXl2n159qDKXlv
GoPAmS3DBV53wjhXmU209abzEfA5cXQI/ZBz0GqtYUWkJDWixoFY5ez8YHeUNsMG
21aWVNansk88yxzNeAjysjPVqOtKDtS+5T1mQVtYjeIx36GqxHNpfzQl3uA20PIK
16C4QxtV43Ao3KHmheZv3YQcBzuB9O4y4BNP+mDzr4yh2kG/m7YBDmYKan90J3kB
yQIDAQAB
-----END PUBLIC KEY-----
]]

local EC_PRIVATE_KEY = [[
-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQggB4H3G/c76vqKnIG
UNoCYaLy6Ltj+OvTcZWXws80amGhRANCAARF9iaLT1OljLvTd5GP5vKcEfmQcfit
ZxyufXToUOmu1sq/PIe+Rsgn6otGoZ4ilnJlg4ukSCTVYtg94g1bFZ2s
-----END PRIVATE KEY-----
]]

local EC_PUBLIC_KEY = [[
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAERfYmi09TpYy703eRj+bynBH5kHH4
rWccrn106FDprtbKvzyHvkbIJ+qLRqGeIpZyZYOLpEgk1WLYPeINWxWdrA==
-----END PUBLIC KEY-----
]]

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

	-- RS256 algorithm
	local t_rs, err12 = crypto.jwt.encode(payload, RSA_PRIVATE_KEY, "RS256")
	assert.is_nil(err12, "RS256 encode should not error")
	local d_rs, err13 = crypto.jwt.verify(t_rs, RSA_PUBLIC_KEY, "RS256")
	assert.is_nil(err13, "RS256 verify should not error")
	assert.eq(d_rs.sub, "user123", "RS256 preserves claims")

	-- ES256 algorithm
	local t_es, err14 = crypto.jwt.encode(payload_header, EC_PRIVATE_KEY, "ES256")
	assert.is_nil(err14, "ES256 encode should not error")
	local d_es, err15 = crypto.jwt.verify(t_es, EC_PUBLIC_KEY, "ES256")
	assert.is_nil(err15, "ES256 verify should not error")
	assert.eq(d_es.sub, "user123", "ES256 preserves claims")

	local _, err16 = crypto.jwt.verify(t_es, EC_PUBLIC_KEY, "HS256")
	assert.not_nil(err16, "ES256 token must not verify as HS256")

	local _, err17 = crypto.jwt.verify(t_rs, EC_PUBLIC_KEY, "ES256")
	assert.not_nil(err17, "RS256 token must not verify as ES256")

	local _, err18 = crypto.jwt.encode(payload, RSA_PRIVATE_KEY, "ES256")
	assert.not_nil(err18, "RSA key must not sign ES256")

	-- Unsupported algorithm
	local t_bad, err19 = crypto.jwt.encode(payload, "secret", "none")
	assert.is_nil(t_bad, "unsupported algorithm returns no token")
	assert.not_nil(err19, "unsupported algorithm should error")

	-- _header.alg must match the signing algorithm
	local t_alg, err20 = crypto.jwt.encode({ exp = exp, _header = { alg = "none" } }, "secret")
	assert.is_nil(t_alg, "mismatched _header.alg returns no token")
	assert.not_nil(err20, "mismatched _header.alg should error")

	return true
end

return { main = main }
