-- SPDX-License-Identifier: MPL-2.0

-- Worker that receives CANCEL event and verifies its structure
local time = require("time")

local function main()
	local events_ch = process.events()
	if not events_ch then
		return false, "failed to get events channel"
	end

	local inbox_ch = process.inbox()
	if not inbox_ch then
		return false, "failed to get inbox channel"
	end

	-- Notify caller we're ready
	local timeout = time.after("5s")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= inbox_ch then
		return false, "timeout waiting for caller"
	end

	local msg = result.value
	local caller_pid = msg:payload():data() :: string
	process.send(caller_pid, "ready", process.pid())

	-- Wait for CANCEL event
	timeout = time.after("10s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for cancel"
	end

	local event = result.value
	if not event then
		return false, "nil event"
	end

	local topic = event.kind
	if topic ~= process.event.CANCEL then
		return false, "expected CANCEL, got: " .. tostring(topic)
	end

	-- Extract cancel event details
	local from = event.from
	local payload = event:payload()

	-- Return details for verification
	return {
		topic = topic,
		from = from,
		has_payload = payload ~= nil,
		kind = payload and payload.kind or nil,
		has_reason = payload and payload.reason ~= nil or false,
	}
end

return { main = main }
