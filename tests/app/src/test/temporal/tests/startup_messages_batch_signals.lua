-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local time = require("time")

local function wait_for_exit(events_ch, pid, timeout)
	local deadline = time.after(timeout or "5s")
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

	local workflow_name = "startup-batch-" .. tostring(time.now():unix_nano())

	local spawner = process
		.with_options({})
		:with_name(workflow_name)
		:with_message("increment", { amount = 2 })
		:with_message("increment", { amount = 1 })
		:with_message("increment", { amount = 4 })

	local pid, err = spawner:spawn_monitored(
		"app.test.temporal.workflows:signal_updates_workflow",
		"app.test.temporal:test_worker",
		{ initial = 0 }
	)
	assert.is_nil(err, "spawn with startup batch no error")
	assert.is_string(pid, "spawn returns pid")

	local ok, send_err = process.send(pid, "finish", {})
	assert.is_nil(send_err, "finish send ok")
	assert.eq(ok, true, "finish send returns true")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing exit event")
	end

	assert.is_table(event.result.value, "result value table")
	assert.eq(event.result.value.final_counter, 7, "startup messages applied in workflow state")
	assert.eq(event.result.value.updates_processed, 3, "three startup messages processed")

	return true
end

return { main = main }
