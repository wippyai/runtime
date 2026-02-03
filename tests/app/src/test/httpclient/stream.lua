-- Test: HTTP streaming response
local assert = require("assert2")

local function main()
	local http = require("http_client")

	-- Streaming POST request (to stream-echo which returns chunked response)
	local test_body = string.rep("X", 1024)
	local resp, err = http.post("http://localhost:8085/stream-echo", {
		body = test_body,
		stream = true
	})
	assert.is_nil(err, "streaming POST should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.stream, "stream object returned")

	-- Read data from stream
	local chunks = {}
	local total = 0
	while true do
		local chunk, read_err = resp.stream:read(256)
		if read_err then
			break
		end
		if chunk == nil then
			break
		end
		table.insert(chunks, chunk)
		total = total + #chunk
	end

	assert.eq(total, 1024, "read 1024 bytes total")

	-- Close stream
	local _, close_err = resp.stream:close()
	assert.is_nil(close_err, "close should not error")

	-- Verify content matches
	local content = table.concat(chunks)
	assert.eq(content, test_body, "streamed content matches")

	return true
end

return { main = main }
