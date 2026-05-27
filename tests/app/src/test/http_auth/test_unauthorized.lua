-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	local res, err = http.get("http://localhost:8085/auth/protected")
	if err then
		return false, "request error: " .. tostring(err)
	end

	assert.eq(res.status_code, 401, "should return 401 Unauthorized")
	assert.not_nil(res.body, "response body present")

	local body = json.decode(tostring(res.body))
	assert.not_nil(body, "response should be valid JSON")
	assert.not_nil(body.error, "response should contain error")

	return true
end

return { main = main }
