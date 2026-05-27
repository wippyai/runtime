-- SPDX-License-Identifier: MPL-2.0

-- With no overlay network requested and no app-default configured, http.get
-- must hit the clearnet target. The network.select permission must not be
-- consulted at all in this path (the check only fires on explicit selection).

local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	local resp, err = http.get("http://localhost:8085/hello", {
		timeout = "2s",
	})
	assert.is_nil(err, "clearnet GET failed: " .. tostring(err))
	assert.not_nil(resp, "clearnet response returned")
	assert.eq(resp.status_code, 200, "clearnet status is 200")
	assert.not_nil(resp.body, "response body present")

	local data = json.decode(tostring(resp.body))
	assert.not_nil(data, "decoded JSON body")
	assert.eq(data.message, "hello world", "hello endpoint answered directly")

	return true
end

return { main = main }
