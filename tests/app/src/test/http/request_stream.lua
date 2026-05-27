-- SPDX-License-Identifier: MPL-2.0

-- Test: HTTP request stream method via test endpoint
local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	-- Test 1: Request with body - should stream successfully
	local body = "Hello from streaming test!"
	local resp, err = http.post("http://localhost:8085/test/request-stream", {
		body = body
	})
	assert.is_nil(err, "POST should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.body, "body present")

	local data = json.decode(tostring(resp.body))
	assert.eq(data.has_body, true, "has_body should be true")
	assert.eq(data.total_size, #body, "total_size matches body length")
	assert.eq(data.content, body, "content matches original body")
	assert.is_nil(data.stream_error, "no stream error")
	assert.is_nil(data.read_error, "no read error")
	assert.is_nil(data.close_error, "no close error")

	-- Test 2: Large body - multiple chunks
	local large_body = string.rep("X", 2048)
	local resp2, err2 = http.post("http://localhost:8085/test/request-stream", {
		body = large_body
	})
	assert.is_nil(err2, "large POST should not error")
	assert.not_nil(resp2.body, "body present")

	local data2 = json.decode(tostring(resp2.body))
	assert.eq(data2.total_size, 2048, "large body size correct")
	assert.ok(data2.chunk_count >= 1, "read in chunks")
	assert.eq(data2.content, large_body, "large content matches")

	-- Test 3: Empty body
	local resp3, err3 = http.post("http://localhost:8085/test/request-stream", {
		body = ""
	})
	assert.is_nil(err3, "empty POST should not error")
	assert.not_nil(resp3.body, "body present")

	local data3 = json.decode(tostring(resp3.body))
	-- Empty body still has has_body=false typically
	assert.eq(data3.total_size or 0, 0, "empty body size is 0")

	return true
end

return { main = main }
