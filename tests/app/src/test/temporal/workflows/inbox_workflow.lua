-- SPDX-License-Identifier: MPL-2.0

-- Workflow that receives signals via process.inbox()
local function main(input)
	local my_pid = process.pid()

	-- Wait for a message via process inbox
	local inbox = process.inbox()
	local msg, ok = inbox:receive()

	if not ok then
		return {
			pid = my_pid,
			status = "timeout",
			error = "no message received"
		}
	end

	-- Get the actual payload data (msg:payload() returns a userdata wrapper)
	local payload_data = nil
	local p = msg:payload()
	if p then
		payload_data = p:data()
	end

	return {
		pid = my_pid,
		received_topic = msg:topic(),
		received_payload = payload_data,
		status = "received"
	}
end

return main
