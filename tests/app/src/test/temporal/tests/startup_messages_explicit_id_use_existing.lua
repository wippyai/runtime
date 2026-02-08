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

	local workflow_id = "startup-explicit-id-use-existing-" .. tostring(time.now():unix_nano())

	local first = process
		.with_options({
			["temporal.workflow.id"] = workflow_id,
		})
		:with_message("increment", { amount = 4 })

	local pid, err = first:spawn_monitored(
		"app.test.temporal.workflows:signal_updates_workflow",
		"app.test.temporal:test_worker",
		{ initial = 0 }
	)
	assert.is_nil(err, "first explicit id spawn no error")
	assert.is_string(pid, "first explicit id pid")

	local second = process
		.with_options({
			["temporal.workflow.id"] = workflow_id,
		})
		:with_message("increment", { amount = 1 })

	local pid2, err2 = second:spawn(
		"app.test.temporal.workflows:signal_updates_workflow",
		"app.test.temporal:test_worker",
		{ initial = 999 }
	)
	assert.is_nil(err2, "second explicit id spawn no error")
	assert.eq(pid2, pid, "second explicit id spawn reuses existing workflow")

	local ok, send_err = process.send(pid, "finish", {})
	assert.is_nil(send_err, "finish send ok")
	assert.eq(ok, true, "finish send returns true")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing exit event")
	end

	assert.is_table(event.result.value, "result value table")
	assert.eq(event.result.value.final_counter, 5, "startup messages from both starts applied")
	assert.eq(event.result.value.updates_processed, 2, "two startup messages processed")

	return true
end

return { main = main }
