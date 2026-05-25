-- SPDX-License-Identifier: MPL-2.0

-- Worker executed with process.exec. It starts two async function calls, then
-- receives both payloads through future channels with channel.select.
local funcs = require("funcs")
local time = require("time")

local function main()
	local exec = funcs.new():with_context({
		process_exec_async_select_id = "peas-246",
		process_exec_async_select_called = true,
	})

	local first, first_err = exec:async("app.test.ctx:ctx_reader", { "process_exec_async_select_id" })
	if first_err then
		return { ok = false, stage = "first_start", error = tostring(first_err) }
	end

	local second, second_err = exec:async("app.test.ctx:ctx_reader", { "process_exec_async_select_called" })
	if second_err then
		return { ok = false, stage = "second_start", error = tostring(second_err) }
	end

	local first_ch = first:channel()
	local second_ch = second:channel()
	local timeout = time.after("3s")
	local got_id = false
	local got_marker = false

	while not (got_id and got_marker) do
		local cases = table.create(3, 0)
		if not got_id then cases[#cases + 1] = first_ch:case_receive() end
		if not got_marker then cases[#cases + 1] = second_ch:case_receive() end
		cases[#cases + 1] = timeout:case_receive()
		local selected = channel.select(cases)
		if selected.channel == timeout then
			return { ok = false, stage = "timeout" }
		end
		if selected.ok == false then
			local stage = selected.channel == first_ch and "first_closed" or "second_closed"
			local future = selected.channel == first_ch and first or second
			local ferr, has_error = future:error()
			return { ok = false, stage = stage, has_error = has_error, error = tostring(ferr) }
		end
		if not selected.value then
			return { ok = false, stage = "nil_payload" }
		end

		local data = selected.value:data()
		if selected.channel == first_ch then
			got_id = data.process_exec_async_select_id == "peas-246"
		elseif selected.channel == second_ch then
			got_marker = data.process_exec_async_select_called == true
		end
	end

	return {
		ok = true,
		stage = "done",
		got_id = got_id,
		got_marker = got_marker,
	}
end

return { main = main }
