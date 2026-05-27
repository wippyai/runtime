-- SPDX-License-Identifier: MPL-2.0

-- Test: HTTP GET requests
local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	-- Basic GET request to local hello endpoint
	local resp, err = http.get("http://localhost:8085/hello")
	assert.is_nil(err, "GET should not error")
	assert.not_nil(resp, "response returned")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.headers, "headers present")
	assert.not_nil(resp.body, "body present")

	-- Parse JSON response
	local data = json.decode(tostring(resp.body))
	assert.not_nil(data, "JSON parsed")
	assert.eq(data.message, "hello world", "message field matches")

	return true
end

return { main = main }
