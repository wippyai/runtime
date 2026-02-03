local http = require("http")

local function handler()
	local req, _ = http.request()
	local res, _ = http.response()

	if not req or not res then
		return nil, "Failed to get HTTP context"
	end

	local test_type = req:query("test")

	if test_type == "headers" then
	-- Echo back all request headers
		local result = {
			headers = {},
			content_type = req:content_type(),
			content_length = req:content_length()
		}
		-- Get specific header if provided
		local header_name = req:query("header")
		if header_name then
			result.header_value = req:header(header_name)
		end
		res:set_status(http.STATUS.OK)
		res:write_json(result)
		return

	elseif test_type == "body" then
	-- Echo back request body as JSON
		local has_body = req:has_body()
		local body_json, parse_err = req:body_json()
		res:set_status(http.STATUS.OK)
		res:write_json({
			body_json = body_json,
			parse_error = parse_err and tostring(parse_err) or nil,
			has_body = has_body
		})
		return

	elseif test_type == "query" then
	-- Echo back query params
		local params = req:query_params()
		res:set_status(http.STATUS.OK)
		res:write_json({
			all_params = params,
			specific = req:query("value")
		})
		return

	elseif test_type == "method" then
	-- Echo back request method and path
		res:set_status(http.STATUS.OK)
		res:write_json({
			method = req:method(),
			path = req:path(),
			host = req:host()
		})
		return

	elseif test_type == "sse" then
	-- Test SSE events
		res:write_event({ name = "start", data = { msg = "Starting" }})
		res:write_event({ name = "data", data = { value = 42 }})
		res:write_event({ name = "end", data = { msg = "Done" }})
		return

	elseif test_type == "custom_headers" then
	-- Test setting response headers
		res:set_header("X-Custom-Header", "custom-value")
		res:set_header("X-Another", "another-value")
		res:set_content_type(http.CONTENT.JSON)
		res:set_status(http.STATUS.OK)
		res:write_json({ success = true })
		return

	elseif test_type == "status_codes" then
		local code = tonumber(req:query("code")) or 200
		res:set_status(code)
		res:write_json({ requested_code = code })
		return
	end

	-- Default: echo everything
	res:set_status(http.STATUS.OK)
	res:write_json({
		method = req:method(),
		path = req:path(),
		has_body = req:has_body(),
		content_type = req:content_type()
	})
end

return {
	handler = handler
}
