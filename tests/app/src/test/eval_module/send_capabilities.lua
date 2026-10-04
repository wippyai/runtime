-- SPDX-License-Identifier: MPL-2.0

-- Test: in cap mode eval code may address PIDs handed to it (its parent,
-- message senders, children) and nothing else.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local inbox = process.inbox()
	local self = process.pid()

	local child, err = eval.spawn([[
		return { main = function(parent)
			local ok, send_err = process.send(parent, "from_eval", "hello")
			if not ok then return "parent send failed: " .. tostring(send_err) end
			local forged, forged_err = process.send("{app:processes|forged}", "x", "y")
			return tostring(forged_err)
		end }
	]], { input = self, modules = { "process" }, monitor_only = true })
	assert.is_nil(err, "spawn succeeds")

	local timeout = time.after("5s")
	local got_message, result = false, nil
	while not (got_message and result) do
		local selected = channel.select { inbox:case_receive(), events:case_receive(), timeout:case_receive() }
		if selected.channel == inbox then
			assert.eq(selected.value:topic(), "from_eval", "eval reached its parent")
			assert.eq(selected.value:from(), child, "message comes from the eval process")
			got_message = true
		elseif selected.channel == events then
			if selected.value.kind == process.event.EXIT and selected.value.from == child then
				result = selected.value.result
			end
		else
			error("timed out waiting for eval process")
		end
	end
	assert.is_nil(result.error, "eval process succeeds")
	assert.contains(result.value, "not allowed to send", "forged pid is refused")

	local denied, derr = eval.spawn([[
		return { main = function(parent)
			local ok, send_err = process.send(parent, "x", "y")
			return tostring(send_err)
		end }
	]], { input = self, modules = { "process" }, send = "deny", monitor_only = true })
	assert.is_nil(derr, "spawn with send=deny succeeds")
	timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "deny-mode eval exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == denied then
			assert.contains(selected.value.result.value, "not allowed to send", "send=deny refuses the parent")
			break
		end
	end
	return true
end

return { main = main }
