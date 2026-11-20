-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main()
	local ready = process.listen("ready")
	local events = process.events()
	local worker = assert(process.spawn_monitored("app.test.process:events_select_worker", "app:processes", process.pid()))
	local started = channel.select { ready:case_receive(), time.after("5s"):case_receive() }
	assert(started.channel == ready and started.value == true, "worker did not start")

	assert(process.cancel(worker, "first"))
	assert(process.cancel(worker, "second"))
	assert(process.send(worker, "release", true))

	local exit = channel.select { events:case_receive(), time.after("5s"):case_receive() }
	assert(exit.channel == events and exit.value.kind == process.event.EXIT, "worker did not exit")
	assert(exit.value.from == worker, "unexpected worker")
	assert(exit.value.result.error == nil, tostring(exit.value.result.error))
	assert(exit.value.result.value == true, "worker did not receive both cancellations")
	return true
end

return { main = main }
