local assert = require("assert2")
local time = require("time")

local function wait_for_filtered_event(events_ch, matcher, timeout)
	local deadline = time.after(timeout or "2s")
	while true do
		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, nil
		end
		local event = result.value
		if matcher(event) then
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

	time.sleep(200 * time.MILLISECOND)

	local ok, mon_err = process.monitor(pid)
	assert.is_nil(mon_err, "monitor no error")
	assert.eq(ok, true, "monitor returns true")

	ok, mon_err = process.unmonitor(pid)
	assert.is_nil(mon_err, "unmonitor no error")
	assert.eq(ok, true, "unmonitor returns true")

	ok, err = process.terminate(pid)
	assert.is_nil(err, "terminate no error")
	assert.eq(ok, true, "terminate returns true")

	local event = wait_for_filtered_event(events_ch, function(e)
		return e.from == pid and e.kind == process.event.EXIT
	end, "1500ms")
	assert.is_nil(event, "no exit should be delivered after unmonitor")

	return true
end

return { main = main }
