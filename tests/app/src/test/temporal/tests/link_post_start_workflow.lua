-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local time = require("time")

local function wait_for_kind(events_ch, pid, kind, timeout)
	local deadline = time.after(timeout or "5s")
	while true do
		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for " .. tostring(kind) .. " from " .. tostring(pid)
		end
		local event = result.value
		if event.from == pid and event.kind == kind then
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

	-- Ensure link attaches after workflow has already started.
	time.sleep(200 * time.MILLISECOND)

	ok, link_err = process.link(pid)
	assert.is_nil(link_err, "link no error")
	assert.eq(ok, true, "link returns true")

	ok, err = process.terminate(pid)
	assert.is_nil(err, "terminate no error")
	assert.eq(ok, true, "terminate returns true")

	local event, wait_err = wait_for_kind(events_ch, pid, process.event.LINK_DOWN, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing link_down event")
	end
	assert.not_nil(event.result.error, "linked terminated workflow should return error")

	return true
end

return { main = main }
