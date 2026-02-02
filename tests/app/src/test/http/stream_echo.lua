-- Test: Local stream echo endpoint
local assert = require("assert2")

local function main()
	local http = require("http_client")

	-- Test 1: Basic stream echo with small body
	local body = "Hello, streaming world!"
	local resp, err = http.post("http://localhost:8085/stream-echo", {
		body = body,
		stream = true
	})
	assert.is_nil(err, "POST should not error")
	assert.eq(resp.status_code, 200, "status code 200")
	assert.not_nil(resp.stream, "stream object returned")

	-- Read echoed data
	local chunks = {}
	while true do
		local chunk, read_err = resp.stream:read(1024)
		if read_err then
			break
		end
		if chunk == nil then
			break
		end
		table.insert(chunks, chunk)
	end
	resp.stream:close()

	local echoed = table.concat(chunks)
	assert.eq(echoed, body, "echoed body matches")

	-- Test 2: Larger body
	local large_body = string.rep("X", 4096)
	local resp2, err2 = http.post("http://localhost:8085/stream-echo", {
		body = large_body,
		stream = true
	})
	assert.is_nil(err2, "large POST should not error")
	assert.eq(resp2.status_code, 200, "status code 200 for large body")

	assert.not_nil(resp2.stream, "stream object for large body")
	local chunks2 = {}
	local total = 0
	while true do
		local chunk, read_err = resp2.stream:read(1024)
		if read_err then
			break
		end
		if chunk == nil then
			break
		end
		table.insert(chunks2, chunk)
		total = total + #chunk
	end
	resp2.stream:close()

	assert.eq(total, 4096, "large body echoed completely")
	local echoed2 = table.concat(chunks2)
	assert.eq(echoed2, large_body, "large echoed body matches")

	-- Test 3: Non-streaming response (without stream=true)
	local resp3, err3 = http.post("http://localhost:8085/stream-echo", {
		body = "test data"
	})
	assert.is_nil(err3, "non-streaming POST should not error")
	assert.eq(resp3.status_code, 200, "status code 200")
	assert.eq(resp3.body, "test data", "body echoed in non-streaming mode")

	return true
end

return { main = main }
