-- SPDX-License-Identifier: MPL-2.0

local http = require("http")

local function handler()
	local req, _ = http.request()
	local res, _ = http.response()

	if not req or not res then
		return nil, "Failed to get HTTP context"
	end

	-- Get test type from query param
	local test_type = req:query("test")
	local result = { test = test_type }

	if test_type == "write_then_status" then
	-- Write first, then try to set status (should error)
		res:write("some data")
		local err = res:set_status(http.STATUS.OK)
		if err then
			result.error = tostring(err)
			result.error_kind = err:kind()
		end
		-- Response is already written, just return
		return

	elseif test_type == "write_then_header" then
	-- Write first, then try to set header (should error)
		res:write("some data")
		local err = res:set_header("X-Test", "value")
		if err then
			result.error = tostring(err)
			result.error_kind = err:kind()
		end
		return

	elseif test_type == "write_then_content_type" then
	-- Write first, then try to set content type (should error)
		res:write("some data")
		local err = res:set_content_type("text/plain")
		if err then
			result.error = tostring(err)
			result.error_kind = err:kind()
		end
		return

	elseif test_type == "write_then_transfer" then
	-- Write first, then try to set transfer mode (should error)
		res:write("some data")
		local err = res:set_transfer(http.TRANSFER.CHUNKED)
		if err then
			result.error = tostring(err)
			result.error_kind = err:kind()
		end
		return

	elseif test_type == "sse_auto" then
	-- Test SSE auto-headers
		res:write_event({ name = "test", data = { message = "hello" }})
		res:write_event({ name = "done", data = { complete = true }})
		return

	elseif test_type == "normal" then
	-- Normal flow: headers before write
		res:set_header("X-Custom", "test-value")
		res:set_content_type(http.CONTENT.JSON)
		res:set_status(http.STATUS.OK)
		res:write_json({ success = true, test = test_type })
		return
	end

	-- Default: return test info
	res:set_status(http.STATUS.OK)
	res:write_json(result)
end

return {
	handler = handler
}
