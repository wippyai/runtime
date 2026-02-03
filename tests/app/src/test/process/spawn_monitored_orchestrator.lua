-- Orchestrator process that spawns a monitored worker
-- Reproduces the gov service pattern

local function main(args)
	local respond_to = args.respond_to
	local request_id = args.request_id
	local work_data = args.work_data

	-- Get events channel first
	local events_ch = process.events()

	-- Spawn monitored worker
	local worker_pid, err = process.spawn_monitored(
		"app.test.process:spawn_monitored_worker",
		"app:processes",
		{
			work_data = work_data
		}
	)

	if err then
	-- Send error response
		process.send(nil, respond_to, {
			request_id = request_id,
			success = false,
			error = tostring(err)
		})
		return false
	end

	-- Wait for worker EXIT event
	local event = events_ch:receive()

	if event.kind == process.event.EXIT and event.from == worker_pid then
	-- Worker completed, send response back
		local response = {
			request_id = request_id,
			success = event.result.error == nil,
			result = event.result.value
		}

		process.send(nil, respond_to, response)
		return true
	end

	-- Unexpected event
	process.send(nil, respond_to, {
		request_id = request_id,
		success = false,
		error = "unexpected event: " .. tostring(event.kind)
	})
	return false
end

return { main = main }
