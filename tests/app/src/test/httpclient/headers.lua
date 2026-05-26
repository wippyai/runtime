-- SPDX-License-Identifier: MPL-2.0

-- Test: HTTP custom headers
local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	-- Test sending custom headers
	local resp, err = http.get("http://localhost:8085/test/echo?test=headers&header=X-Custom-Header", {
		headers = {["X-Custom-Header"] = "custom-value"}
	})
	assert.is_nil(err, "GET with headers should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.body, "response body present")

	local data = json.decode(tostring(resp.body))
	assert.eq(data.header_value, "custom-value", "custom header received")

	-- Test response headers (custom_headers endpoint sets X-Custom-Header)
	local resp2, err2 = http.get("http://localhost:8085/test/echo?test=custom_headers")
	assert.is_nil(err2, "GET custom_headers should not error")
	assert.eq(resp2.status_code, 200, "status code 200")
	assert.eq(resp2.headers["X-Custom-Header"], "custom-value", "response has custom header")
	assert.eq(resp2.headers["X-Another"], "another-value", "response has another header")

	return true
end

return { main = main }
