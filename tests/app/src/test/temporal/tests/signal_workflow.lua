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

	local pid, err = process.spawn_monitored(
		"app.test.temporal.workflows:signal_workflow",
		"app.test.temporal:test_worker",
		{ suite = "temporal" }
	)
	assert.is_nil(err, "spawn_monitored no error")
	assert.is_string(pid, "got pid")

	local ok, send_err = process.send(pid, "add_job", { id = "job-1", data = "hello" })
	assert.is_nil(send_err, "send add_job ok")
	assert.eq(ok, true, "send returns true")

	ok, send_err = process.send(pid, "exit", { reason = "done" })
	assert.is_nil(send_err, "send exit ok")
	assert.eq(ok, true, "send returns true")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	assert.eq(event.from, pid, "exit from pid")
	assert.is_nil(event.result.error, "no error on exit")
	assert.is_table(event.result.value, "result value table")
	assert.eq(event.result.value.total_jobs, 1, "one job processed")
	assert.is_table(event.result.value.results, "results table")

	return true
end

return { main = main }
