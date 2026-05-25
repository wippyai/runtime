-- SPDX-License-Identifier: MPL-2.0

-- Test: the native SigV4 signer matches AWS's published "get-vanilla" test
-- vector, proving HMAC-SHA256 raw chaining (hash module) is sufficient to
-- sign AWS requests natively.
local assert = require("assert2")
local sigv4 = require("sigv4")
local hash = require("hash")

local function main()
	-- AWS sig-v4-test-suite "get-vanilla".
	local result = sigv4.sign({
		method = "GET",
		uri = "/",
		query = "",
		headers = {
			host = "example.amazonaws.com",
			["x-amz-date"] = "20150830T123600Z",
		},
		body = "",
		access_key = "AKIDEXAMPLE",
		secret_key = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		region = "us-east-1",
		service = "service",
		amz_date = "20150830T123600Z",
	})

	assert.eq(result.signed_headers, "host;x-amz-date", "signed headers")
	assert.eq(result.payload_hash,
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"empty payload hash")
	assert.eq(hash.sha256(result.canonical_request),
		"bb579772317eb040ac9ed261061d46c1f17a8133879d6129b6e1c25292927e63",
		"canonical request hash matches AWS vector")
	assert.eq(result.signature,
		"5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31",
		"get-vanilla signature matches AWS vector")
	assert.eq(result.authorization,
		"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request"
		.. ", SignedHeaders=host;x-amz-date"
		.. ", Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31",
		"authorization header matches")

	return true
end

return { main = main }
