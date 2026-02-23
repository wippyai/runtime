-- SPDX-License-Identifier: MPL-2.0

-- Workflow that spawns a child and waits for EXIT event

local function main(input)
	local my_pid = process.pid()

	-- Subscribe to events channel to receive EXIT
	local events_ch = process.events()

	-- Spawn child workflow
	local child_pid, err = process.spawn("app.test.temporal.workflows:child_workflow", "test-queue", {
		message = "hello from parent"
	})

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

	return {
		pid = my_pid,
		child_pid = child_pid,
		event_kind = event.kind,
		event_from = event.from,
		child_value = child_value,
		child_error = child_error,
		status = "completed"
	}
end

return main
