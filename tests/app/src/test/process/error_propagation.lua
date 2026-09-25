-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local time = require("time")

local function check_error(err)
	assert.not_nil(err, "child error exists")
	assert.eq(err:kind(), errors.INVALID, "child kind")
	assert.eq(err:retryable(), false, "child retryable")
	assert.eq(err:message(), "bad declaration", "child message")
	assert.eq(err:details().field, "target", "child detail")
	assert.eq(err:details().nested.code, "required", "child nested detail")
	assert.eq(errors.is(err, errors.INVALID), true, "child errors.is")
end

local function main()
	local value, err = process.exec("app.test.process:raised_error_worker", "app:processes", false)
	assert.is_nil(value, "body failure has nil value")
	check_error(err)

	value, err = process.exec("app.test.process:raised_error_worker", "app:processes", true)
	assert.is_nil(value, "yielded body failure has nil value")
	check_error(err)

	value, err = process.exec("app.test.process:raise_on_load_worker", "app:processes")
	assert.is_nil(value, "startup failure has nil value")
	check_error(err)

	local events = process.events()
	local child, spawn_err = process.spawn_monitored("app.test.process:raised_error_worker", "app:processes", true)
	assert.is_nil(spawn_err, "monitored child spawned")
	local timeout = time.after("3s")
	local selected = channel.select { events:case_receive(), timeout:case_receive() }
	assert.eq(selected.channel, events, "child exit event received")
	assert.eq(selected.value.kind, process.event.EXIT, "child exited")
	assert.eq(selected.value.from, child, "expected child exited")
	check_error(selected.value.result.error)
	return true
end

return { main = main }
