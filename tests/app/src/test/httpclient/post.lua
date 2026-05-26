-- SPDX-License-Identifier: MPL-2.0

-- Test: HTTP POST requests
local assert = require("assert2")
local http = require("http_client")
local json = require("json")

local function main()
-- POST with JSON body
	local post_data = json.encode({name = "test", value = 42})
	local resp, err = http.post("http://localhost:8085/test/echo?test=body", {
		headers = {["Content-Type"] = "application/json"},
		body = post_data
	})
	assert.is_nil(err, "POST should not error")
	assert.eq(resp.status_code, 200, "POST status 200")
	assert.not_nil(resp.body, "response body present")

	local result = json.decode(tostring(resp.body))
	assert.is_nil(result.parse_error, "no parse error")
	assert.not_nil(result.body_json, "JSON body parsed by server")
	assert.eq(result.body_json.name, "test", "name field correct")
	assert.eq(result.body_json.value, 42, "value field correct")

	-- Test POST to stream echo (echo back body)
	local test_body = "POST body test data"
	local resp2, err2 = http.post("http://localhost:8085/stream-echo", {
		body = test_body
	})
	assert.is_nil(err2, "POST to stream-echo should not error")
	assert.eq(resp2.status_code, 200, "status 200")
	assert.eq(resp2.body, test_body, "body echoed correctly")

	return true
end

return { main = main }
