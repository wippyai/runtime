-- Test: payload error handling
local assert = require("assert2")

local function main()
-- Test that payload.new always succeeds (no errors)
	local p = payload.new({key = "value"})
	assert.not_nil(p, "payload created")

	-- Test data() on LUA format payload returns value directly (no error)
	local data = p:data()
	assert.not_nil(data, "data returned")
	assert.eq(data.key, "value", "data matches")

	-- Test unmarshal() on LUA format payload returns value directly (no error)
	local unmarshaled = p:unmarshal()
	assert.not_nil(unmarshaled, "unmarshal returned")
	assert.eq(unmarshaled.key, "value", "unmarshaled matches")

	-- Test transcode to JSON (should work with transcoder in context)
	local json_p, err = p:transcode(payload.format.JSON)
	if err then
	-- If transcoder not available, error should be structured
		assert.eq(type(err.kind), "function", "error has kind method")
		assert.eq(err:kind(), errors.INTERNAL, "error kind is INTERNAL")
		assert.eq(err:retryable(), false, "error is not retryable")
	else
	-- Transcode succeeded
		assert.not_nil(json_p, "transcoded payload")
		assert.eq(json_p:get_format(), payload.format.JSON, "format is JSON")
	end

	-- Test error from payload wrapping Go error
	local err_payload = payload.new(errors.new("test error"))
	assert.not_nil(err_payload, "error payload created")
	assert.eq(err_payload:get_format(), payload.format.ERROR, "error format")

	return true
end

return { main = main }
