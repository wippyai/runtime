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

	local spawner = process.with_context({
		user_id = "user-1",
		tenant = "tenant-1",
		request_id = "req-1",
	})

	local pid, err = spawner:spawn_monitored(
		"app.test.temporal.workflows:ctx_workflow",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(err, "spawn ctx workflow no error")
	assert.is_string(pid, "got pid")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	assert.eq(event.from, pid, "exit from pid")
	assert.is_table(event.result.value, "result value table")
	assert.eq(event.result.value.user_id, "user-1", "user_id propagated")
	assert.eq(event.result.value.tenant, "tenant-1", "tenant propagated")
	assert.eq(event.result.value.request_id, "req-1", "request_id propagated")
	assert.is_table(event.result.value.all_context, "all_context table")

	return true
end

return { main = main }
