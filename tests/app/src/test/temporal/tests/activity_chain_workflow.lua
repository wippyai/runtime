local assert = require("assert2")
local time = require("time")

local function wait_for_exit(events_ch, pid, timeout)
	local deadline = time.after(timeout or "10s")
	while true do
		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for exit"
		end
		local event = result.value
		if event.from == pid and event.kind == process.event.EXIT then
			return event, nil
		end
	end
end

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	local workflow_name = "activity-chain-" .. tostring(time.now():unix_nano())
	local pid, err = process
		.with_options({})
		:with_name(workflow_name)
		:spawn_monitored(
			"app.test.temporal.workflows:activity_chain_workflow",
			"app.test.temporal:test_worker",
			{
				first_message = "hello",
				id = "7",
				name = "workflow",
			}
		)
	assert.is_nil(err, "spawn should not error")
	assert.is_string(pid, "got workflow pid")

	local event, wait_err = wait_for_exit(events_ch, pid, "10s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing exit event")
	end
	assert.is_nil(event.result.error, "workflow should complete without error")

	local result = event.result.value
	assert.is_table(result, "result should be table")

	assert.eq(result.status, "ok", "workflow status")
	assert.is_table(result.first, "first activity result")
	assert.eq(result.first.message, "hello", "first activity payload")
	assert.is_table(result.second, "second activity result")
	assert.eq(result.second.processed_id, "7", "second activity id")
	assert.eq(result.second.processed_name, "workflow", "second activity name")
	assert.eq(result.second.status, "success", "second activity status")

	return true
end

return { main = main }
