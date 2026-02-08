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

	local pid, err = process.spawn(
		"app.test.temporal.workflows:long_workflow",
		"app.test.temporal:test_worker",
		{ iterations = 1000 }
	)
	assert.is_nil(err, "spawn no error")
	assert.is_string(pid, "got pid")

	-- Ensure monitor attaches after workflow has already started.
	time.sleep(200 * time.MILLISECOND)

	local ok, mon_err = process.monitor(pid)
	assert.is_nil(mon_err, "monitor no error")
	assert.eq(ok, true, "monitor returns true")

	ok, err = process.terminate(pid)
	assert.is_nil(err, "terminate no error")
	assert.eq(ok, true, "terminate returns true")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing exit event")
	end
	assert.not_nil(event.result.error, "terminated workflow should return error")

	return true
end

return { main = main }
