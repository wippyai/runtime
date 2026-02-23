-- SPDX-License-Identifier: MPL-2.0

-- Test: CORS headers on actual requests
local assert = require("assert2")

local function main()
	local http = require("http_client")

	-- Test actual POST request with Origin header
	local resp, err = http.post("http://localhost:8085/stream-echo", {
		body = "test",
		headers = {
			["Origin"] = "http://localhost:5173"
		}
	})

	assert.is_nil(err, "POST request should not error")
	assert.eq(resp.status_code, 200, "should return 200 OK")

	-- Check CORS headers on actual response
	local allow_origin = resp.headers["Access-Control-Allow-Origin"]
	assert.ok(allow_origin ~= nil and allow_origin ~= "", "Access-Control-Allow-Origin should be set on actual request")
	assert.eq(allow_origin, "http://localhost:5173", "should echo back the origin")

	-- Vary header should be present for correct caching
	local vary = resp.headers["Vary"]
	assert.ok(vary ~= nil, "Vary header should be set")

	return true
end

return { main = main }
