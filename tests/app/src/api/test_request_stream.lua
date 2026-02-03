local http = require("http")

local function handler()
	local req, _ = http.request()
	local res, _ = http.response()

	if not req or not res then
		return nil, "Failed to get HTTP context"
	end

	-- Test request stream functionality
	local result = {}

	-- Check has_body
	local has_body = req:has_body()
	result.has_body = has_body

	if not has_body then
		res:set_status(http.STATUS.OK)
		res:write_json(result)
		return
	end

	-- Get stream
	local stream, stream_err = req:stream()
	if stream_err then
		result.stream_error = tostring(stream_err)
		res:set_status(http.STATUS.OK)
		res:write_json(result)
		return
	end

	-- Read all data
	local chunks = {}
	local total_size = 0
	local chunk_count = 0
	while true do
		local chunk, read_err = stream:read(256)
		if read_err then
			result.read_error = tostring(read_err)
			break
		end
		if chunk == nil then
			break
		end
		chunk_count = chunk_count + 1
		total_size = total_size + #chunk
		table.insert(chunks, chunk)
	end

	-- Close stream
	local _, close_err = stream:close()
	if close_err then
		result.close_error = tostring(close_err)
	end

	result.total_size = total_size
	result.chunk_count = chunk_count
	result.content = table.concat(chunks)

	res:set_status(http.STATUS.OK)
	res:write_json(result)
end

return {
	handler = handler
}
