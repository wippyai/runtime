-- SPDX-License-Identifier: MPL-2.0

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
	local ok, set_err = process.set_options({ trap_links = true })
	assert.is_nil(set_err, "set trap_links no error")
	assert.eq(ok, true, "set trap_links returns true")

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

	ok, err = process.link(pid)
	assert.is_nil(err, "link no error")
	assert.eq(ok, true, "link returns true")

	ok, err = process.unlink(pid)
	assert.is_nil(err, "unlink no error")
	assert.eq(ok, true, "unlink returns true")

	ok, err = process.terminate(pid)
	assert.is_nil(err, "terminate no error")
	assert.eq(ok, true, "terminate returns true")

	local event = wait_for_filtered_event(events_ch, function(e)
		return e.from == pid and e.kind == process.event.LINK_DOWN
	end, "1500ms")
	assert.is_nil(event, "no link_down should be delivered after unlink")

	return true
end

return { main = main }
