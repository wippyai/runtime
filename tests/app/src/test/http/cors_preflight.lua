-- SPDX-License-Identifier: MPL-2.0

-- Test: CORS preflight OPTIONS request handling
local assert = require("assert2")

local function main()
	local http = require("http_client")

	-- Test preflight OPTIONS request with CORS headers
	local resp, err = http.request("OPTIONS", "http://localhost:8085/stream-echo", {
		headers = {
			["Origin"] = "http://localhost:5173",
			["Access-Control-Request-Method"] = "POST",
			["Access-Control-Request-Headers"] = "Content-Type"
		}
	})

	assert.is_nil(err, "OPTIONS request should not error")
	assert.eq(resp.status_code, 204, "preflight should return 204 No Content")

	-- Check CORS headers are present
	local allow_origin = resp.headers["Access-Control-Allow-Origin"]
	assert.ok(allow_origin ~= nil and allow_origin ~= "", "Access-Control-Allow-Origin should be set")
	assert.eq(allow_origin, "http://localhost:5173", "should echo back the origin")

	local allow_methods = resp.headers["Access-Control-Allow-Methods"]
	assert.ok(allow_methods ~= nil and allow_methods ~= "", "Access-Control-Allow-Methods should be set")

	local max_age = resp.headers["Access-Control-Max-Age"]
	assert.ok(max_age ~= nil and max_age ~= "", "Access-Control-Max-Age should be set")

	return true
end

return { main = main }
