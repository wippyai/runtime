-- SPDX-License-Identifier: MPL-2.0

-- Test: Server-Sent Events (SSE)
local assert = require("assert2")

local function main()
	local http = require("http_client")

	-- SSE test - get events via streaming response
	local resp, err = http.get("http://localhost:8085/test/echo?test=sse", {
		stream = true
	})
	assert.is_nil(err, "SSE GET should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.stream, "stream object returned")

	-- Read SSE events
	local chunks = {}
	while true do
		local chunk, read_err = resp.stream:read(4096)
		if read_err then
			break
		end
		if chunk == nil then
			break
		end
		table.insert(chunks, chunk)
	end
	resp.stream:close()

	local content = table.concat(chunks)

	-- SSE format should have "event:" and "data:" lines
	assert.ok(content:find("event: start"), "contains start event")
	assert.ok(content:find("event: data"), "contains data event")
	assert.ok(content:find("event: end"), "contains end event")
	assert.ok(content:find('"msg"'), "contains msg field")

	return true
end

return { main = main }
