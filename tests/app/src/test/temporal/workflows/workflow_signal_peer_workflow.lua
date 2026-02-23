-- SPDX-License-Identifier: MPL-2.0

-- Workflow that signals another Temporal workflow process.

local function main(input)
	local target_pid = input and input.target_pid or nil
	local topic = "peer_ping"
	if input ~= nil and type(input.topic) == "string" and input.topic ~= "" then
		topic = input.topic
	end
	local body = input and input.payload or { source = "workflow_signal_peer" }

	if type(target_pid) ~= "string" or target_pid == "" then
		return {
			ok = false,
			error = "target_pid is required"
		}
	end

	local ok, err = process.send(target_pid, topic, body)
	if err ~= nil then
		return {
			ok = false,
			error = tostring(err),
			error_kind = err:kind(),
			error_retryable = err:retryable(),
		}
	end

	return {
		ok = ok,
		topic = topic,
		target = target_pid
	}
end

return main
