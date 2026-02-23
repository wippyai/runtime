-- SPDX-License-Identifier: MPL-2.0

-- Workflow that initiates a roundtrip with a non-temporal process.
local function main(input)
	local target_pid = input and input.target_pid or nil
	if type(target_pid) ~= "string" or target_pid == "" then
		return {
			ok = false,
			error = "target_pid is required",
		}
	end

	local self_pid, pid_err = process.pid()
	if pid_err ~= nil then
		return {
			ok = false,
			error = tostring(pid_err),
		}
	end

	local sent, send_err = process.send(target_pid, "cross_host_ping", {
		from = self_pid,
		probe = "workflow->host",
	})
	if send_err ~= nil then
		return {
			ok = false,
			error = tostring(send_err),
		}
	end
	if sent ~= true then
		return {
			ok = false,
			error = "cross_host_ping send returned false",
		}
	end

	local pong = process.listen("cross_host_pong", { message = true })
	local msg, recv_ok = pong:receive()
	if not recv_ok then
		return {
			ok = false,
			error = "cross_host_pong not received",
		}
	end

	local payload_data = nil
	local p = msg:payload()
	if p ~= nil then
		payload_data = p:data()
	end

	return {
		ok = true,
		self_pid = self_pid,
		received_from = msg:from(),
		received_topic = msg:topic(),
		received_payload = payload_data,
	}
end

return main
