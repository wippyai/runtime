-- Workflow that spawns a child that errors and captures the error

local function main(input)
	local my_pid = process.pid()

	-- Subscribe to events channel to receive EXIT
	local events_ch = process.events()

	-- Spawn child workflow that will error
	local child_pid, err = process.spawn("app.test.temporal.workflows:error_child_workflow", "test-queue", {})

	if err then
		return {
			pid = my_pid,
			status = "spawn_failed",
			error = tostring(err)
		}
	end

	-- Wait for child EXIT event
	local event, ok = events_ch:receive()

	if not ok then
		return {
			pid = my_pid,
			child_pid = child_pid,
			status = "channel_closed",
			error = "events channel closed"
		}
	end

	-- event.result contains {value: ..., error: ...}
	local child_value = event.result and event.result.value
	local child_error = event.result and event.result.error

	-- Extract error metadata using Lua error methods
	local error_kind = nil
	local error_retryable = nil
	local error_message = nil
	if child_error then
	-- Test that error methods work
		if type(child_error.kind) == "function" then
			error_kind = child_error:kind()
		elseif type(child_error) == "userdata" and child_error.kind then
			error_kind = child_error:kind()
		end
		if type(child_error.retryable) == "function" then
			error_retryable = child_error:retryable()
		elseif type(child_error) == "userdata" and child_error.retryable then
			error_retryable = child_error:retryable()
		end
		if type(child_error.message) == "function" then
			error_message = child_error:message()
		elseif type(child_error) == "userdata" and child_error.message then
			error_message = child_error:message()
		end
	end

	return {
		pid = my_pid,
		child_pid = child_pid,
		event_kind = event.kind,
		event_from = event.from,
		child_value = child_value,
		error_kind = error_kind,
		error_retryable = error_retryable,
		error_message = error_message,
		status = "completed"
	}
end

return main
