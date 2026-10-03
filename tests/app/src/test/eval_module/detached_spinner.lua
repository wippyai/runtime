-- SPDX-License-Identifier: MPL-2.0

-- Test: the detached eval lifetime stops a spinning eval that no owner and
-- no step limit would stop within the test.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local launcher, err = process.spawn_monitored("app.test.eval_module:spin_launcher", "app:processes")
	assert.is_nil(err, "launcher starts")

	local timeout = time.after("10s")
	local spinner
	while not spinner do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "launcher exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == launcher then
			assert.is_nil(selected.value.result.error, "launcher succeeds")
			spinner = selected.value.result.value
		end
	end

	assert.ok(process.monitor(spinner), "the detached spinner is running")
	local started = time.now()
	timeout = time.after("10s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "the lifetime stops the spinner")
		if selected.value.kind == process.event.EXIT and selected.value.from == spinner then
			assert.not_nil(selected.value.result.error, "the spinner fails when its lifetime ends")
			assert.ok(time.now():sub(started):seconds() < 8, "it stops at the configured lifetime")
			return true
		end
	end
end

return { main = main }
