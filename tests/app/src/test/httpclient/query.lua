-- SPDX-License-Identifier: MPL-2.0

-- Test: HTTP query parameters
local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	-- Query parameters via options
	local resp, err = http.get("http://localhost:8085/test/echo?test=query", {
		query = {foo = "bar", num = "123", value = "test_value"}
	})
	assert.is_nil(err, "GET with query should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.body, "response body present")

	local data = json.decode(tostring(resp.body))
	assert.eq(data.specific, "test_value", "query param value")
	assert.eq(data.all_params.foo, "bar", "query param foo")
	assert.eq(data.all_params.num, "123", "query param num")

	return true
end

return { main = main }
