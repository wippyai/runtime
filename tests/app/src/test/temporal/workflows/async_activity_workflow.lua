-- SPDX-License-Identifier: MPL-2.0

local funcs = require("funcs")
local time = require("time")

local function main(input)
	input = input or {}

	local future, start_err = funcs.async("app.test.temporal.activities:echo_activity", {
		message = input.message or "async",
	})
	if start_err then
		return {
			status = "start_error",
			error = tostring(start_err),
		}
	end

	local timeout = time.after("5s")
	local result = channel.select {
		future:response():case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return {status = "timeout"}
	end
	if result.ok ~= true then
		return {status = "channel_closed"}
	end

	local payload = result.value
	local data = payload and payload:data() or {}

	local cached, future_err = future:result()
	if future_err then
		return {
			status = "future_error",
			error = tostring(future_err),
		}
	end
	local cached_data = cached and cached:data() or {}

	return {
		status = "ok",
		message = data.message,
		cached_message = cached_data.message,
	}
end

return main
