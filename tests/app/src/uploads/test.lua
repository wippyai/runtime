-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local http = require("http_client")
local json = require("json")

local function main()
	local file_content = "Hello from uploaded file!\nThis is line 2.\n"
	local filename = "test_upload.txt"

	local resp, err = http.post("http://localhost:8085/test/upload", {
		files = {
			{
				name = "file",
				filename = filename,
				content_type = "text/plain",
				content = file_content
			}
		}
	})

	assert.is_nil(err, "POST should not error: " .. tostring(err))
	assert.ok(resp, "response should not be nil")
	assert.ok(resp.body, "response body should not be nil: status=" .. tostring(resp.status_code))
	assert.eq(resp.status_code, 200, "status code should be 200, body: " .. tostring(resp.body))

	local data = json.decode(tostring(resp.body))
	assert.ok(data, "json decode should succeed, body: " .. tostring(resp.body))
	assert.is_nil(data.error, "no error in response: " .. tostring(data.error))
	assert.eq(data.filename, filename, "filename matches")
	assert.eq(data.size, #file_content, "size matches")
	assert.eq(data.written, true, "file was written")
	assert.eq(data.written_size, #file_content, "written size matches")
	assert.eq(data.written_content, file_content, "written content matches")

	return true
end

return { main = main }
